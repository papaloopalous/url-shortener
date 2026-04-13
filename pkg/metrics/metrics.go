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
	EventURLCreated      = "url_created"
	EventURLRedirected   = "url_redirected"
	EventURLDeleted      = "url_deleted"
	EventCacheHit        = "cache_hit"
	EventCacheMiss       = "cache_miss"
	EventOutboxPublished = "outbox_published"
	EventOutboxFailed    = "outbox_failed"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shortener",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests partitioned by method, path and status.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shortener",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	shortenerEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shortener",
		Name:      "events_total",
		Help:      "Business-level shortener events.",
	}, []string{"event"})

	outboxPending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "shortener",
		Name:      "outbox_pending_total",
		Help:      "Current number of pending outbox events.",
	})
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
	shortenerEvents.WithLabelValues(event).Inc()
}

func SetOutboxPending(n float64) {
	outboxPending.Set(n)
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
