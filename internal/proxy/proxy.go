package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"gateway-service/internal/balancer"
	"gateway-service/pkg/metrics"
)

type Proxy struct {
	balancer *balancer.LeastConn
	name     string // "auth" | "shortener" | "analytics"
	log      *slog.Logger
}

func NewProxy(b *balancer.LeastConn, name string, log *slog.Logger) *Proxy {
	return &Proxy{balancer: b, name: name, log: log}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	inst := p.balancer.Next()
	if inst == nil {
		p.log.ErrorContext(r.Context(), "no healthy instances", "upstream", p.name)
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	defer inst.Done()

	metrics.IncUpstreamRequests(p.name)

	target, err := url.Parse(inst.Addr())
	if err != nil {
		p.log.ErrorContext(r.Context(), "invalid upstream addr", "addr", inst.Addr(), "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if clientIP := r.Header.Get("X-Forwarded-For"); clientIP != "" {
				req.Header.Set("X-Forwarded-For", clientIP)
			} else {
				req.Header.Set("X-Forwarded-For", r.RemoteAddr)
			}

			if userID := r.Header.Get("X-User-ID"); userID != "" {
				req.Header.Set("X-User-ID", userID)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			p.log.ErrorContext(req.Context(), "upstream error",
				"upstream", p.name,
				"addr", inst.Addr(),
				"err", err,
			)
			http.Error(w, `{"error":"bad gateway"}`, http.StatusBadGateway)
		},
	}

	rp.ServeHTTP(w, r)
}
