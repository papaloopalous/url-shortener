package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	EventRateLimitAllowed  = "ratelimit_allowed"
	EventRateLimitRejected = "ratelimit_rejected"
	EventRateLimitError    = "ratelimit_error"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests partitioned by method, path and status.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gateway",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	gatewayEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "events_total",
		Help:      "Gateway-level events (rate limit, etc.).",
	}, []string{"event"})

	upstreamRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "upstream_requests_total",
		Help:      "Total requests routed to each upstream service.",
	}, []string{"upstream"})

	balancerActiveConns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "balancer_active_conns",
		Help:      "Current active connections per upstream instance.",
	}, []string{"upstream", "addr"})

	instanceHealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "instance_healthy",
		Help:      "Health status per upstream instance (1=healthy, 0=unhealthy).",
	}, []string{"upstream", "addr"})
)

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		path := r.Pattern
		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rw.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}

func IncEvent(event string) {
	gatewayEvents.WithLabelValues(event).Inc()
}

func IncUpstreamRequests(upstream string) {
	upstreamRequestsTotal.WithLabelValues(upstream).Inc()
}

func SetActiveConns(upstream, addr string, n float64) {
	balancerActiveConns.WithLabelValues(upstream, addr).Set(n)
}

func SetInstanceHealth(upstream, addr string, healthy bool) {
	v := 0.0
	if healthy {
		v = 1.0
	}
	instanceHealthy.WithLabelValues(upstream, addr).Set(v)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
