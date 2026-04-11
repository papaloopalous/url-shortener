package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	grpcRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shortener",
		Subsystem: "grpc",
		Name:      "requests_total",
		Help:      "Total gRPC calls partitioned by method and gRPC status code.",
	}, []string{"method", "code"})

	grpcRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shortener",
		Subsystem: "grpc",
		Name:      "request_duration_seconds",
		Help:      "gRPC call latency.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method"})

	shortenerEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shortener",
		Name:      "events_total",
		Help:      "Business-level shortener events.",
	}, []string{"event"})
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

func RecordGRPC(method, code string, d time.Duration) {
	grpcRequestsTotal.WithLabelValues(method, code).Inc()
	grpcRequestDuration.WithLabelValues(method).Observe(d.Seconds())
}

func IncEvent(event string) {
	shortenerEvents.WithLabelValues(event).Inc()
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
