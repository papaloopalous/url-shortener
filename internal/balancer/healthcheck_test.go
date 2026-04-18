package balancer

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gateway-service/internal/config"
)

var testLog = slog.New(slog.NewTextHandler(os.Stdout, nil))

func cfg(interval, timeout time.Duration) config.HealthCheckConfig {
	return config.HealthCheckConfig{Interval: interval, Timeout: timeout}
}

func newInst(addr string) *instance {
	i := &instance{addr: addr}
	i.setHealthy(true)
	return i
}

func TestHealthCheck_HealthyInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	inst := newInst(srv.URL)
	hc := &HealthChecker{
		instances: []*instance{inst},
		interval:  time.Second,
		timeout:   time.Second,
		client:    &http.Client{Timeout: time.Second},
		log:       testLog,
	}

	hc.checkAll(context.Background())

	if !inst.isHealthy() {
		t.Error("instance should be healthy after 200 response")
	}
}

func TestHealthCheck_UnhealthyInstance_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	inst := newInst(srv.URL)
	hc := NewHealthChecker([]*instance{inst}, cfg(time.Second, time.Second), testLog)

	hc.checkAll(context.Background())

	if inst.isHealthy() {
		t.Error("instance should be unhealthy after 500 response")
	}
}

func TestHealthCheck_ConnectionRefused(t *testing.T) {
	inst := newInst("http://127.0.0.1:19999")
	hc := NewHealthChecker([]*instance{inst}, cfg(time.Second, 200*time.Millisecond), testLog)

	hc.checkAll(context.Background())

	if inst.isHealthy() {
		t.Error("instance should be unhealthy when connection is refused")
	}
}

func TestHealthCheck_Recovery(t *testing.T) {
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	inst := newInst(srv.URL)
	hc := NewHealthChecker([]*instance{inst}, cfg(time.Second, time.Second), testLog)

	hc.checkAll(context.Background())
	if !inst.isHealthy() {
		t.Fatal("should be healthy initially")
	}

	healthy = false
	hc.checkAll(context.Background())
	if inst.isHealthy() {
		t.Fatal("should be unhealthy after 500")
	}

	healthy = true
	hc.checkAll(context.Background())
	if !inst.isHealthy() {
		t.Error("should be healthy again after recovery")
	}
}

func TestHealthCheck_LogsOnlyOnTransition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	inst := newInst(srv.URL)
	hc := NewHealthChecker([]*instance{inst}, cfg(time.Second, time.Second), testLog)

	for i := 0; i < 5; i++ {
		hc.checkAll(context.Background())
	}
	if !inst.isHealthy() {
		t.Error("should remain healthy")
	}
}
