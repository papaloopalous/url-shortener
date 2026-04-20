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

	rediscache "analytics-service/internal/adapters/cache/redis"
	"analytics-service/internal/adapters/db/postgres"
	kafkaadapter "analytics-service/internal/adapters/kafka"
	"analytics-service/internal/config"
	httpcontroller "analytics-service/internal/controller/http"
	"analytics-service/internal/usecase"
	"analytics-service/internal/worker"
	pgclient "analytics-service/pkg/client/postgres"
	redisclient "analytics-service/pkg/client/redis"
	"analytics-service/pkg/logging"
	"analytics-service/pkg/tracing"

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
		ServiceName:  "analytics-service",
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

	consumer := kafkaadapter.NewConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.ClickEventsTopic,
		cfg.Kafka.GroupID,
	)
	defer func() {
		if err := consumer.Close(); err != nil {
			log.Error("consumer shutdown", "err", err)
		}
	}()

	log.Info("kafka consumer created",
		"topic", cfg.Kafka.ClickEventsTopic,
		"group_id", cfg.Kafka.GroupID,
	)

	clickRepo := postgres.NewClickRepo(pool)
	inboxRepo := postgres.NewInboxRepo(pool)
	statsCache := rediscache.NewStatsCache(redisClient)

	statsUsecase := usecase.NewStatsUsecase(clickRepo, statsCache, cfg.Stats.CacheTTL, log)

	pool_ := worker.NewPool(consumer, inboxRepo, statsCache, log, cfg.Worker.Concurrency)
	go pool_.Run(ctx)
	log.Info("worker pool started", "concurrency", cfg.Worker.Concurrency)

	handler := httpcontroller.NewStatsHandler(statsUsecase, log)
	router := httpcontroller.NewRouter(handler, log)

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
