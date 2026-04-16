package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env          string
	OTLPEndpoint string
	HTTP         HTTPConfig
	PG           PGConfig
	Redis        RedisConfig
	Kafka        KafkaConfig
	Worker       WorkerConfig
	Stats        StatsConfig
}

type HTTPConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type PGConfig struct {
	DSN      string
	MaxConns int32
	MinConns int32
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type KafkaConfig struct {
	Brokers          []string
	ClickEventsTopic string
	GroupID          string
}

type WorkerConfig struct {
	Concurrency int
}

type StatsConfig struct {
	CacheTTL time.Duration
}

func Load() (*Config, error) {
	pgMaxConns, _ := strconv.ParseInt(getEnv("PG_MAX_CONNS", "20"), 10, 32)
	pgMinConns, _ := strconv.ParseInt(getEnv("PG_MIN_CONNS", "2"), 10, 32)

	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "2"))

	concurrency, err := strconv.Atoi(getEnv("WORKER_CONCURRENCY", "10"))
	if err != nil {
		return nil, fmt.Errorf("invalid WORKER_CONCURRENCY: %w", err)
	}

	cacheTTL, err := time.ParseDuration(getEnv("STATS_CACHE_TTL", "5m"))
	if err != nil {
		return nil, fmt.Errorf("invalid STATS_CACHE_TTL: %w", err)
	}

	return &Config{
		Env:          getEnv("APP_ENV", "development"),
		OTLPEndpoint: getEnv("OTLP_ENDPOINT", "localhost:4318"),
		HTTP: HTTPConfig{
			Addr:         getEnv("HTTP_ADDR", ":8083"),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		PG: PGConfig{
			DSN:      mustEnv("POSTGRES_DSN"),
			MaxConns: int32(pgMaxConns),
			MinConns: int32(pgMinConns),
		},
		Redis: RedisConfig{
			Addr:     mustEnv("REDIS_ADDR"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		Kafka: KafkaConfig{
			Brokers:          splitComma(mustEnv("KAFKA_BROKERS")),
			ClickEventsTopic: getEnv("KAFKA_CLICK_EVENTS_TOPIC", "click-events"),
			GroupID:          getEnv("KAFKA_GROUP_ID", "analytics-service"),
		},
		Worker: WorkerConfig{
			Concurrency: concurrency,
		},
		Stats: StatsConfig{
			CacheTTL: cacheTTL,
		},
	}, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		panic(fmt.Sprintf("required env var %q is not set", key))
	}
	return v
}

func splitComma(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if part := s[start:i]; part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	return result
}
