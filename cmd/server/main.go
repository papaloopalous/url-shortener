package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shortener-service/internal/adapters/cache/redis"
	"shortener-service/internal/adapters/db/postgres"
	grpcadapter "shortener-service/internal/adapters/grpc"
	"shortener-service/internal/adapters/kafka"
	"shortener-service/internal/config"
	httpcontroller "shortener-service/internal/controller/http"
	"shortener-service/internal/outbox"
	"shortener-service/internal/usecase"
	pgclient "shortener-service/pkg/client/postgres"
	redisclient "shortener-service/pkg/client/redis"
	"shortener-service/pkg/logging"
	"shortener-service/pkg/tracing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logging.NewLogger(cfg.Env)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := tracing.Setup(ctx, tracing.Config{
		ServiceName:  "shortener-service",
		Environment:  cfg.Env,
		OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		log.Warn("tracing setup failed, continuing without tracing", "error", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				log.Error("tracing shutdown failed", "error", err)
			}
		}()
	}

	pool, err := pgclient.NewPool(ctx, pgclient.Config{
		DSN:      cfg.PG.DSN,
		MaxConns: cfg.PG.MaxConns,
		MinConns: cfg.PG.MinConns,
	})
	if err != nil {
		return fmt.Errorf("postgres pool: %w", err)
	}
	defer pool.Close()
	log.Info("postgres connected")

	db, err := sql.Open("pgx", cfg.PG.DSN)
	if err != nil {
		return fmt.Errorf("open db for migrations: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	db.Close() //nolint:errcheck
	log.Info("migrations applied")

	redisClient, err := redisclient.NewClient(ctx, redisclient.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		return fmt.Errorf("redis client: %w", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Error("redis shutdown", "err", err)
		}
	}()
	log.Info("redis connected")

	producer := kafka.NewProducer(cfg.Kafka.Brokers)
	defer func() {
		if err := producer.Close(); err != nil {
			log.Error("producer shutdown", "err", err)
		}
	}()
	log.Info("kafka producer created", "brokers", cfg.Kafka.Brokers)

	authClient, err := grpcadapter.NewAuthGRPCClient(cfg.Auth.GRPCAddr)
	if err != nil {
		return fmt.Errorf("auth grpc client: %w", err)
	}
	log.Info("auth grpc client created", "addr", cfg.Auth.GRPCAddr)

	urlRepo := postgres.NewURLRepo(pool)
	outboxRepo := postgres.NewOutboxRepo(pool)
	urlCache := redis.NewURLCache(redisClient)

	urlUsecase := usecase.NewURLUsecase(
		urlRepo,
		outboxRepo,
		urlCache,
		cfg.URL.BaseURL,
		cfg.URL.DefaultTTL,
		log,
	)

	poller := outbox.NewPoller(outboxRepo, producer, log, cfg.Outbox.BatchSize, cfg.Outbox.PollInterval)
	go poller.Run(ctx)
	log.Info("outbox poller started")

	handler := httpcontroller.NewURLHandler(urlUsecase, log)
	router := httpcontroller.NewRouter(handler, authClient, log)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server starting", "addr", cfg.HTTP.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server error: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http server shutdown failed", "error", err)
		return fmt.Errorf("http server shutdown: %w", err)
	}

	log.Info("server shut down gracefully")
	return nil
}
