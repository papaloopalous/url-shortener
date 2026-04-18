package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env          string
	OTLPEndpoint string
	HTTP         HTTPConfig
	Auth         AuthConfig
	Upstreams    UpstreamsConfig
	Redis        RedisConfig
	RateLimit    RateLimitConfig
	HealthCheck  HealthCheckConfig
}

type HTTPConfig struct {
	Addr string
}

type AuthConfig struct {
	GRPCAddr string
}

type UpstreamsConfig struct {
	AuthAddrs      []string
	ShortenerAddrs []string
	AnalyticsAddrs []string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type RateLimitConfig struct {
	Requests int64
	Window   time.Duration
	Enabled  bool
}

type HealthCheckConfig struct {
	Interval time.Duration
	Timeout  time.Duration
}

func Load() (*Config, error) {
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "3"))
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB: %w", err)
	}

	rateLimitRequests, err := strconv.ParseInt(getEnv("RATE_LIMIT_REQUESTS", "100"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_REQUESTS: %w", err)
	}

	rateLimitWindow, err := time.ParseDuration(getEnv("RATE_LIMIT_WINDOW", "1m"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_WINDOW: %w", err)
	}

	rateLimitEnabled, err := strconv.ParseBool(getEnv("RATE_LIMIT_ENABLED", "true"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_ENABLED: %w", err)
	}

	hcInterval, err := time.ParseDuration(getEnv("HEALTHCHECK_INTERVAL", "10s"))
	if err != nil {
		return nil, fmt.Errorf("invalid HEALTHCHECK_INTERVAL: %w", err)
	}

	hcTimeout, err := time.ParseDuration(getEnv("HEALTHCHECK_TIMEOUT", "2s"))
	if err != nil {
		return nil, fmt.Errorf("invalid HEALTHCHECK_TIMEOUT: %w", err)
	}

	return &Config{
		Env:          getEnv("APP_ENV", "development"),
		OTLPEndpoint: getEnv("OTLP_ENDPOINT", "localhost:4318"),
		HTTP: HTTPConfig{
			Addr: getEnv("HTTP_ADDR", ":8080"),
		},
		Auth: AuthConfig{
			GRPCAddr: mustEnv("AUTH_GRPC_ADDR"),
		},
		Upstreams: UpstreamsConfig{
			AuthAddrs:      splitComma(mustEnv("AUTH_ADDRS")),
			ShortenerAddrs: splitComma(mustEnv("SHORTENER_ADDRS")),
			AnalyticsAddrs: splitComma(mustEnv("ANALYTICS_ADDRS")),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		RateLimit: RateLimitConfig{
			Requests: rateLimitRequests,
			Window:   rateLimitWindow,
			Enabled:  rateLimitEnabled,
		},
		HealthCheck: HealthCheckConfig{
			Interval: hcInterval,
			Timeout:  hcTimeout,
		},
	}, nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env variable %q is not set", key))
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
