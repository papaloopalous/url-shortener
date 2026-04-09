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
	GRPC         GRPCConfig
	PG           PGConfig
	Redis        RedisConfig
	JWT          JWTConfig
}

type HTTPConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type GRPCConfig struct {
	Addr string
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

type JWTConfig struct {
	Secret          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	BcryptCost      int
}

func Load() (*Config, error) {
	bcryptCost, err := strconv.Atoi(getEnv("BCRYPT_COST", "12"))
	if err != nil {
		return nil, fmt.Errorf("invalid BCRYPT_COST: %w", err)
	}

	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	pgMaxConns, _ := strconv.ParseInt(getEnv("PG_MAX_CONNS", "20"), 10, 32)
	pgMinConns, _ := strconv.ParseInt(getEnv("PG_MIN_CONNS", "2"), 10, 32)

	return &Config{
		Env:          getEnv("APP_ENV", "development"),
		OTLPEndpoint: getEnv("OTLP_ENDPOINT", "localhost:4318"),
		HTTP: HTTPConfig{
			Addr:         getEnv("HTTP_ADDR", ":8081"),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		GRPC: GRPCConfig{
			Addr: getEnv("GRPC_ADDR", ":9091"),
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
		JWT: JWTConfig{
			Secret:          mustEnv("JWT_SECRET"),
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 30 * 24 * time.Hour,
			BcryptCost:      bcryptCost,
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
