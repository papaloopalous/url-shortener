package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"gateway-service/internal/balancer"
	"gateway-service/internal/proxy"
	"log/slog"
	"os"
)

var testLog = slog.New(slog.NewTextHandler(os.Stdout, nil))

func newProxy(t *testing.T, srv *httptest.Server) *proxy.Proxy {
	t.Helper()
	lb, err := balancer.NewLeastConn([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return proxy.NewProxy(lb, "test", testLog)
}

func TestProxy_RequestReachesUpstream(t *testing.T) {
	var called atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if !called.Load() {
		t.Error("upstream handler was not called")
	}
}

func TestProxy_XForwardedForAdded(t *testing.T) {
	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	p.ServeHTTP(httptest.NewRecorder(), req)

	if gotHeader == "" {
		t.Error("X-Forwarded-For should be set")
	}
}

func TestProxy_XUserIDForwarded(t *testing.T) {
	var gotUserID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.Header.Set("X-User-ID", "user-42")
	p.ServeHTTP(httptest.NewRecorder(), req)

	if gotUserID != "user-42" {
		t.Errorf("X-User-ID not forwarded; got %q", gotUserID)
	}
}

func TestProxy_DoneCalledAfterResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	lb, err := balancer.NewLeastConn([]string{upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	p := proxy.NewProxy(lb, "test", testLog)

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	p.ServeHTTP(httptest.NewRecorder(), req)

	if lb.Len() != 1 {
		t.Error("balancer pool size changed unexpectedly")
	}
}

func TestProxy_UnavailableUpstream_Returns502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	upstream.Close()

	p := newProxy(t, upstream)

	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}
