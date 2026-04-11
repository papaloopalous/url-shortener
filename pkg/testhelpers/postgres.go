package testhelpers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var globalPool *pgxpool.Pool

func RunWithPostgres(m *testing.M, migrationsDir string) {
	ctx := context.Background()

	var container *tcpostgres.PostgresContainer
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "integration: testcontainers panic: %v, skipping\n", r)
				os.Exit(0)
			}
		}()
		container, err = tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("testdb"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),

			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2),
			),
		)
	}()

	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: start postgres: %v, skipping\n", err)
		os.Exit(0)
	}
	defer container.Terminate(ctx) //nolint:errcheck

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: get dsn: %v\n", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: create pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "integration: ping postgres: %v\n", err)
		os.Exit(1)
	}

	db := stdlib.OpenDBFromPool(pool)
	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "integration: goose dialect: %v\n", err)
		os.Exit(1)
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "integration: goose up: %v\n", err)
		os.Exit(1)
	}

	globalPool = pool
	os.Exit(m.Run())
}

func MustGetPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if globalPool == nil {
		t.Fatal("pool not initialised, call testhelpers.RunWithPostgres in TestMain")
	}
	return globalPool
}
