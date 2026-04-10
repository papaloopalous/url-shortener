package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	cacheadapter "auth-service/internal/adapters/cache/redis"
	pgadapter "auth-service/internal/adapters/db/postgres"
	"auth-service/internal/config"
	grpcctrl "auth-service/internal/controller/grpc"
	httpctrl "auth-service/internal/controller/http"
	"auth-service/internal/usecase"
	pgclient "auth-service/pkg/client/postgres"
	redisclient "auth-service/pkg/client/redis"
	"auth-service/pkg/logging"
	"auth-service/pkg/tracing"
	pb "auth-service/proto/auth"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic("load config: " + err.Error())
	}

	log := logging.NewLogger(cfg.Env)
	log.Info("starting auth-service",
		"env", cfg.Env,
		"http", cfg.HTTP.Addr,
		"grpc", cfg.GRPC.Addr,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, tracing.Config{
		ServiceName:    "auth-service",
		ServiceVersion: "0.1.0",
		Environment:    cfg.Env,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		log.Error("init tracing", "err", err)
		os.Exit(1)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutCtx); err != nil {
			log.Error("shutdown tracing", "err", err)
		}
	}()

	pool, err := pgclient.NewPool(ctx, pgclient.Config{
		DSN:      cfg.PG.DSN,
		MaxConns: cfg.PG.MaxConns,
		MinConns: cfg.PG.MinConns,
	})
	if err != nil {
		log.Error("connect postgres", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := redisclient.NewClient(ctx, redisclient.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		log.Error("connect redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// ── Migrations ───────────────────────────────────────────────────────────

	sqlDB, err := sql.Open("pgx", cfg.PG.DSN)
	if err != nil {
		log.Error("open db for migrations", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Error("goose dialect", "err", err)
		os.Exit(1)
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		log.Error("goose up", "err", err)
		os.Exit(1)
	}
	log.Info("migrations applied")

	userRepo := pgadapter.NewUserRepo(pool)
	sessionRepo := pgadapter.NewSessionRepo(pool)
	tokenCache := cacheadapter.NewTokenCacheAdapter(rdb)
	tokenMgr := usecase.NewJWTManager(cfg.JWT.Secret)

	authUC := usecase.NewAuthUsecase(
		userRepo, sessionRepo, tokenCache, tokenMgr,
		cfg.JWT.AccessTokenTTL, cfg.JWT.RefreshTokenTTL, cfg.JWT.BcryptCost,
	)

	authCtrl := httpctrl.NewAuthController(authUC)
	router := httpctrl.NewRouter(authCtrl, log)

	httpSrv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	go func() {
		log.Info("http listening", "addr", cfg.HTTP.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http error", "err", err)
			stop()
		}
	}()

	grpcSrv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterAuthServiceServer(grpcSrv,
		grpcctrl.NewAuthGRPCServer(tokenMgr, tokenCache, log),
	)
	reflection.Register(grpcSrv)

	lis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Error("grpc listen", "addr", cfg.GRPC.Addr, "err", err)
		os.Exit(1)
	}

	go func() {
		log.Info("grpc listening", "addr", cfg.GRPC.Addr)
		if err := grpcSrv.Serve(lis); err != nil {
			log.Error("grpc error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	grpcSrv.GracefulStop()
	log.Info("grpc stopped")

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	log.Info("auth-service stopped")
}
