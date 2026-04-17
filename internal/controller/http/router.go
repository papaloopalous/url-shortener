package http

import (
	"log/slog"
	"net/http"

	"gateway-service/internal/auth"
	"gateway-service/internal/proxy"
	"gateway-service/internal/ratelimit"
	"gateway-service/pkg/metrics"

	"github.com/go-chi/chi/v5"
)

func NewRouter(
	authProxy *proxy.Proxy,
	shortenerProxy *proxy.Proxy,
	analyticsProxy *proxy.Proxy,
	authClient auth.AuthClient,
	limiter ratelimit.LimiterIface,
	log *slog.Logger,
) http.Handler {
	r := chi.NewRouter()

	r.Use(RecoveryMiddleware(log))
	r.Use(TracingMiddleware)
	r.Use(metrics.HTTPMiddleware)
	r.Use(RequestIDMiddleware)
	r.Use(LoggerMiddleware(log))

	r.Get("/healthz", Healthz)
	r.Handle("/metrics", metrics.Handler())

	r.Group(func(r chi.Router) {
		r.Use(RateLimitMiddleware(limiter, log))

		r.Post("/auth/register", authProxy.ServeHTTP)
		r.Post("/auth/login", authProxy.ServeHTTP)
		r.Post("/auth/refresh", authProxy.ServeHTTP)

		r.Group(func(r chi.Router) {
			r.Use(JWTAuthMiddleware(authClient))
			r.Delete("/auth/logout", authProxy.ServeHTTP)
		})

		r.Get("/{code}", shortenerProxy.ServeHTTP)

		r.Group(func(r chi.Router) {
			r.Use(JWTAuthMiddleware(authClient))
			r.Post("/urls", shortenerProxy.ServeHTTP)
			r.Get("/urls", shortenerProxy.ServeHTTP)
			r.Delete("/urls", shortenerProxy.ServeHTTP)
		})

		r.Get("/stats/{code}", analyticsProxy.ServeHTTP)
	})

	return r
}

func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
}
