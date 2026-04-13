package http

import (
	"log/slog"
	"net/http"

	"shortener-service/internal/domain/service"
	"shortener-service/pkg/metrics"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(handler *URLHandler, authClient service.AuthClient, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(RecoveryMiddleware(log))
	r.Use(TracingMiddleware)
	r.Use(metrics.HTTPMiddleware)
	r.Use(RequestIDMiddleware)
	r.Use(LoggerMiddleware(log))
	r.Use(chimiddleware.Compress(5))

	r.Get("/healthz", Healthz)
	r.Handle("/metrics", metrics.Handler())
	r.Get("/{code}", handler.Redirect)

	r.Group(func(r chi.Router) {
		r.Use(JWTAuthMiddleware(authClient))

		r.Post("/urls", handler.Create)
		r.Get("/urls", handler.List)
		r.Delete("/urls", handler.BatchDelete)
	})

	return r
}
