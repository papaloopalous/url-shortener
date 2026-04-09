package testhelpers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var globalRedis *redis.Client

func RunWithRedis(m *testing.M) {
	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		fmt.Fprintf(os.Stderr, "start redis container: %v\n", err)
		os.Exit(1)
	}
	defer container.Terminate(ctx) //nolint:errcheck

	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get redis endpoint: %v\n", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close() //nolint:errcheck

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ping redis: %v\n", err)
		os.Exit(1)
	}

	globalRedis = rdb
	os.Exit(m.Run())
}

func MustGetRedis(t *testing.T) *redis.Client {
	t.Helper()
	if globalRedis == nil {
		t.Fatal("redis client not initialised, call testhelpers.RunWithRedis in TestMain")
	}
	return globalRedis
}
