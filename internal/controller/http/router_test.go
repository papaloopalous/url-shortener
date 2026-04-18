package http_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gateway-service/internal/auth"
	"gateway-service/internal/balancer"
	controller "gateway-service/internal/controller/http"
	"gateway-service/internal/proxy"
	"gateway-service/internal/ratelimit"
)

var testLog = slog.New(slog.NewTextHandler(os.Stdout, nil))

type fakeAuthClient struct {
	userID string
	err    error
}

func (f *fakeAuthClient) ValidateToken(_ context.Context, _ string) (string, error) {
	return f.userID, f.err
}

var _ auth.AuthClient = (*fakeAuthClient)(nil)

type stubLimiter struct {
	allowed   bool
	remaining int64
	err       error
}

func (s *stubLimiter) Allow(_ context.Context, _ string) (bool, int64, error) {
	return s.allowed, s.remaining, s.err
}
func (s *stubLimiter) RetryAfter() int64 { return 60 }
func (s *stubLimiter) Limit() int64      { return 100 }

var _ ratelimit.LimiterIface = (*stubLimiter)(nil)

func passLimiter() ratelimit.LimiterIface   { return &stubLimiter{allowed: true, remaining: 99} }
func rejectLimiter() ratelimit.LimiterIface { return &stubLimiter{allowed: false} }
func errorLimiter() ratelimit.LimiterIface {
	return &stubLimiter{err: errors.New("redis: connection refused"), allowed: true}
}

type upstreamRecorder struct {
	called bool
	userID string
}

func newUpstream(t *testing.T, name string) (*upstreamRecorder, *proxy.Proxy) {
	t.Helper()
	rec := &upstreamRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.called = true
		rec.userID = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	lb, err := balancer.NewLeastConn([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return rec, proxy.NewProxy(lb, name, testLog)
}

func newRouter(
	t *testing.T,
	authClient auth.AuthClient,
	limiter ratelimit.LimiterIface,
) (http.Handler, *upstreamRecorder, *upstreamRecorder, *upstreamRecorder) {
	t.Helper()
	authRec, authProxy := newUpstream(t, "auth")
	shortRec, shortProxy := newUpstream(t, "shortener")
	analyRec, analyProxy := newUpstream(t, "analytics")
	router := controller.NewRouter(authProxy, shortProxy, analyProxy, authClient, limiter, testLog)
	return router, authRec, shortRec, analyRec
}

func do(t *testing.T, h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "1.2.3.4:5678"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouter_Login_GoesToAuth(t *testing.T) {
	router, authRec, _, _ := newRouter(t, &fakeAuthClient{}, passLimiter())
	do(t, router, http.MethodPost, "/auth/login", nil)
	if !authRec.called {
		t.Error("POST /auth/login should reach auth upstream")
	}
}

func TestRouter_Redirect_GoesToShortener(t *testing.T) {
	router, _, shortRec, _ := newRouter(t, &fakeAuthClient{}, passLimiter())
	do(t, router, http.MethodGet, "/abc123", nil)
	if !shortRec.called {
		t.Error("GET /{code} should reach shortener upstream")
	}
}

func TestRouter_Stats_GoesToAnalytics(t *testing.T) {
	router, _, _, analyRec := newRouter(t, &fakeAuthClient{}, passLimiter())
	do(t, router, http.MethodGet, "/stats/abc123", nil)
	if !analyRec.called {
		t.Error("GET /stats/{code} should reach analytics upstream")
	}
}

func TestRouter_PostUrls_WithoutJWT_Returns401(t *testing.T) {
	router, _, _, _ := newRouter(t, &fakeAuthClient{}, passLimiter())
	rec := do(t, router, http.MethodPost, "/urls", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRouter_PostUrls_WithJWT_ForwardsXUserID(t *testing.T) {
	var gotUserID string
	shortSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(shortSrv.Close)

	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(authSrv.Close)

	authLB, _ := balancer.NewLeastConn([]string{authSrv.URL})
	shortLB, _ := balancer.NewLeastConn([]string{shortSrv.URL})
	analyLB, _ := balancer.NewLeastConn([]string{authSrv.URL})

	router := controller.NewRouter(
		proxy.NewProxy(authLB, "auth", testLog),
		proxy.NewProxy(shortLB, "shortener", testLog),
		proxy.NewProxy(analyLB, "analytics", testLog),
		&fakeAuthClient{userID: "user-99"},
		passLimiter(),
		testLog,
	)

	do(t, router, http.MethodPost, "/urls", map[string]string{
		"Authorization": "Bearer valid-token",
	})

	if gotUserID != "user-99" {
		t.Errorf("X-User-ID not forwarded to shortener; got %q", gotUserID)
	}
}

func TestRouter_Logout_WithoutJWT_Returns401(t *testing.T) {
	router, _, _, _ := newRouter(t, &fakeAuthClient{}, passLimiter())
	rec := do(t, router, http.MethodDelete, "/auth/logout", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRouter_RateLimitExceeded_Returns429(t *testing.T) {
	router, _, _, _ := newRouter(t, &fakeAuthClient{}, rejectLimiter())
	rec := do(t, router, http.MethodPost, "/auth/login", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header is missing")
	}
}

func TestRouter_Healthz_Returns200_BypassesRateLimit(t *testing.T) {
	router, _, _, _ := newRouter(t, &fakeAuthClient{}, rejectLimiter())
	rec := do(t, router, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRouter_RedisUnavailable_FailOpen(t *testing.T) {
	router, authRec, _, _ := newRouter(t, &fakeAuthClient{}, errorLimiter())
	do(t, router, http.MethodPost, "/auth/login", nil)
	if !authRec.called {
		t.Error("request should reach upstream when rate limiter errors (fail open)")
	}
}

func TestRouter_InvalidJWT_Returns401(t *testing.T) {
	router, _, _, _ := newRouter(t,
		&fakeAuthClient{err: errors.New("invalid token")},
		passLimiter(),
	)
	rec := do(t, router, http.MethodPost, "/urls", map[string]string{
		"Authorization": "Bearer bad-token",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
