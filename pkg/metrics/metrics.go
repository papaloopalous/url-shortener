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
	EventClickProcessed = "click_processed"
	EventClickDuplicate = "click_duplicate"
	EventClickFailed    = "click_failed"
	EventCacheHit       = "cache_hit"
	EventCacheMiss      = "cache_miss"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "analytics",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests partitioned by method, path and status.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "analytics",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	analyticsEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "analytics",
		Name:      "events_total",
		Help:      "Business-level analytics events.",
	}, []string{"event"})

	workerActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "analytics",
		Name:      "worker_active",
		Help:      "Number of currently active worker goroutines.",
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
	analyticsEvents.WithLabelValues(event).Inc()
}

func WorkerActive(delta float64) {
	workerActive.Add(delta)
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
