package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway-service/internal/auth"
	"gateway-service/internal/balancer"
	"gateway-service/internal/config"
	controller "gateway-service/internal/controller/http"
	"gateway-service/internal/proxy"
	"gateway-service/internal/ratelimit"
	redisclient "gateway-service/pkg/client/redis"
	"gateway-service/pkg/logging"
	"gateway-service/pkg/tracing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway-service: %v\n", err)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, tracing.Config{
		ServiceName:  "gateway-service",
		OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		log.Warn("tracing setup failed — continuing without traces", "err", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTracing(shutdownCtx); err != nil {
				log.Error("tracing shutdown", "err", err)
			}
		}()
	}

	redisClient, err := redisclient.NewClient(ctx, redisclient.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer redisClient.Close()

	authClient, err := auth.NewClient(cfg.Auth.GRPCAddr)
	if err != nil {
		return fmt.Errorf("auth grpc client: %w", err)
	}
	defer authClient.Close()

	limiter := ratelimit.NewLimiter(redisClient, cfg.RateLimit)

	authLB, authHC, err := balancer.NewLeastConnWithConfig(cfg.Upstreams.AuthAddrs, cfg.HealthCheck, log)
	if err != nil {
		return fmt.Errorf("auth balancer: %w", err)
	}
	authHC.WithUpstream("auth")

	shortenerLB, shortHC, err := balancer.NewLeastConnWithConfig(cfg.Upstreams.ShortenerAddrs, cfg.HealthCheck, log)
	if err != nil {
		return fmt.Errorf("shortener balancer: %w", err)
	}
	shortHC.WithUpstream("shortener")

	analyticsLB, analyHC, err := balancer.NewLeastConnWithConfig(cfg.Upstreams.AnalyticsAddrs, cfg.HealthCheck, log)
	if err != nil {
		return fmt.Errorf("analytics balancer: %w", err)
	}
	analyHC.WithUpstream("analytics")

	go authHC.Run(ctx)
	go shortHC.Run(ctx)
	go analyHC.Run(ctx)

	authProxy := proxy.NewProxy(authLB, "auth", log)
	shortenerProxy := proxy.NewProxy(shortenerLB, "shortener", log)
	analyticsProxy := proxy.NewProxy(analyticsLB, "analytics", log)

	router := controller.NewRouter(
		authProxy,
		shortenerProxy,
		analyticsProxy,
		authClient,
		limiter,
		log,
	)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("starting gateway-service", "addr", cfg.HTTP.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("gateway-service stopped")
	return nil
}
