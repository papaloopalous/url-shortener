package http

import (
	"log/slog"
	"net/http"

	"auth-service/pkg/metrics"
)

func NewRouter(auth *AuthController, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /auth/register", auth.Register)
	mux.HandleFunc("POST /auth/login", auth.Login)
	mux.HandleFunc("POST /auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /auth/logout", auth.Logout)

	mux.Handle("GET /metrics", metrics.Handler())

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	var h http.Handler = mux
	h = Logger(log)(h)
	h = RequestID(h)
	h = Metrics(h)
	h = Tracing(h)
	return h
}
