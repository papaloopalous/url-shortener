package http

import (
	"log/slog"
	"net/http"

	"analytics-service/pkg/metrics"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(handler *StatsHandler, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(RecoveryMiddleware(log))
	r.Use(TracingMiddleware)
	r.Use(metrics.HTTPMiddleware)
	r.Use(RequestIDMiddleware)
	r.Use(LoggerMiddleware(log))
	r.Use(chimiddleware.Compress(5))

	r.Get("/healthz", Healthz)
	r.Handle("/metrics", metrics.Handler())
	r.Get("/stats/{code}", handler.GetStats)

	return r
}
