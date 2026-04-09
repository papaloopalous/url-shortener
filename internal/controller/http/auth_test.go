package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	httpctrl "auth-service/internal/controller/http"
	"auth-service/internal/controller/http/dto"
	"auth-service/internal/domain/entity"
	"auth-service/internal/usecase"
	"auth-service/internal/usecase/mocks"
	"auth-service/pkg/logging"
)

func newRouter(t *testing.T) (http.Handler, *mocks.MockUserRepository, *mocks.MockSessionRepository, *mocks.MockTokenCache, *mocks.MockTokenManager) {
	t.Helper()
	ctrl := gomock.NewController(t)
	users := mocks.NewMockUserRepository(ctrl)
	sessions := mocks.NewMockSessionRepository(ctrl)
	cache := mocks.NewMockTokenCache(ctrl)
	tokens := mocks.NewMockTokenManager(ctrl)
	uc := usecase.NewAuthUsecase(
		users, sessions, cache, tokens,
		15*time.Minute, 7*24*time.Hour, bcrypt.MinCost,
	)
	return httpctrl.NewRouter(httpctrl.NewAuthController(uc), logging.NewLogger("development")),
		users, sessions, cache, tokens
}

func postJSON(t *testing.T, router http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(w.Body).Decode(dst))
}

func TestRegisterHandler(t *testing.T) {
	t.Run("201 on success", func(t *testing.T) {
		router, users, sessions, _, tokens := newRouter(t)

		users.EXPECT().FindByEmail(gomock.Any(), "alice@example.com").Return(nil, entity.ErrUserNotFound)
		users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		tokens.EXPECT().GenerateAccess(gomock.Any(), 15*time.Minute).Return("acc.tok", "jti", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/register", map[string]string{
			"email": "alice@example.com", "password": "StrongPass1!",
		})

		assert.Equal(t, http.StatusCreated, w.Code)
		var resp dto.TokenResponse
		decodeJSON(t, w, &resp)
		assert.Equal(t, "acc.tok", resp.AccessToken)
		assert.Equal(t, "Bearer", resp.TokenType)
	})

	t.Run("409 on duplicate email", func(t *testing.T) {
		router, users, _, _, _ := newRouter(t)
		users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(&entity.User{}, nil)

		w := postJSON(t, router, "/auth/register", map[string]string{
			"email": "dup@example.com", "password": "StrongPass1!",
		})
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("422 when password too short", func(t *testing.T) {
		router, _, _, _, _ := newRouter(t)
		w := postJSON(t, router, "/auth/register", map[string]string{
			"email": "x@x.com", "password": "short",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("400 on malformed JSON", func(t *testing.T) {
		router, _, _, _, _ := newRouter(t)
		req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString("{bad"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestLoginHandler(t *testing.T) {
	t.Run("200 on success", func(t *testing.T) {
		router, users, sessions, _, tokens := newRouter(t)

		hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
		user := &entity.User{PasswordHash: string(hash)}

		users.EXPECT().FindByEmail(gomock.Any(), "bob@example.com").Return(user, nil)
		tokens.EXPECT().GenerateAccess(gomock.Any(), 15*time.Minute).Return("tok", "jti", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/login", map[string]string{
			"email": "bob@example.com", "password": "correct",
		})
		assert.Equal(t, http.StatusOK, w.Code)
		var resp dto.TokenResponse
		decodeJSON(t, w, &resp)
		assert.Equal(t, "tok", resp.AccessToken)
	})

	t.Run("401 on wrong password", func(t *testing.T) {
		router, users, _, _, _ := newRouter(t)
		hash, _ := bcrypt.GenerateFromPassword([]byte("real"), bcrypt.MinCost)
		users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(
			&entity.User{PasswordHash: string(hash)}, nil)

		w := postJSON(t, router, "/auth/login", map[string]string{
			"email": "x@x.com", "password": "wrong",
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("401 on unknown email (no enumeration)", func(t *testing.T) {
		router, users, _, _, _ := newRouter(t)
		users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(nil, entity.ErrUserNotFound)

		w := postJSON(t, router, "/auth/login", map[string]string{
			"email": "ghost@x.com", "password": "pw",
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestRefreshHandler(t *testing.T) {
	t.Run("200 on valid token", func(t *testing.T) {
		router, _, sessions, _, tokens := newRouter(t)

		session := &entity.RefreshSession{ExpiresAt: time.Now().Add(24 * time.Hour)}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeByID(gomock.Any(), gomock.Any()).Return(nil)
		tokens.EXPECT().GenerateAccess(gomock.Any(), 15*time.Minute).Return("new.tok", "new-jti", nil)
		sessions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/refresh", map[string]string{
			"refresh_token": "valid-raw-token",
		})
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("401 on token reuse", func(t *testing.T) {
		router, _, sessions, _, _ := newRouter(t)

		revokedAt := time.Now().Add(-time.Hour)
		session := &entity.RefreshSession{
			ExpiresAt: time.Now().Add(24 * time.Hour),
			RevokedAt: &revokedAt,
		}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeAllByUserID(gomock.Any(), gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/refresh", map[string]string{
			"refresh_token": "already-used-token",
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestLogoutHandler(t *testing.T) {
	t.Run("204 on success", func(t *testing.T) {
		router, _, sessions, cache, _ := newRouter(t)

		session := &entity.RefreshSession{ExpiresAt: time.Now().Add(time.Hour)}
		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(session, nil)
		sessions.EXPECT().RevokeByID(gomock.Any(), gomock.Any()).Return(nil)
		cache.EXPECT().RevokeJTI(gomock.Any(), "jti-xyz", gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/logout", map[string]any{
			"refresh_token": "raw-token", "jti": "jti-xyz", "access_ttl_seconds": 300,
		})
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("204 even when session not found", func(t *testing.T) {
		router, _, sessions, cache, _ := newRouter(t)

		sessions.EXPECT().FindByTokenHash(gomock.Any(), gomock.Any()).Return(nil, entity.ErrSessionNotFound)
		cache.EXPECT().RevokeJTI(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		w := postJSON(t, router, "/auth/logout", map[string]any{
			"refresh_token": "any", "jti": "any-jti", "access_ttl_seconds": 60,
		})
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

func TestHealthz(t *testing.T) {
	router, _, _, _, _ := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
