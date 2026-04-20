package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"shortener-service/internal/controller/http/dto"
	"shortener-service/internal/domain/entity"
	"shortener-service/internal/domain/service"
	"shortener-service/internal/usecase"
	"shortener-service/internal/usecase/mocks"

	"github.com/google/uuid"
	gomock "go.uber.org/mock/gomock"

	handler "shortener-service/internal/controller/http"
)

type fakeAuthClient struct {
	userID string
	err    error
}

func (f *fakeAuthClient) ValidateToken(_ context.Context, _ string) (string, error) {
	return f.userID, f.err
}

func setupRouter(
	ctrl *gomock.Controller,
	urlRepo *mocks.MockURLRepository,
	outbox *mocks.MockOutboxRepository,
	cache *mocks.MockURLCache,
	authClient service.AuthClient,
) http.Handler {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	uc := usecase.NewURLUsecase(urlRepo, outbox, cache, "http://short.ly", 90*24*time.Hour, log)
	h := handler.NewURLHandler(uc, log)
	return handler.NewRouter(h, authClient, log)
}

func TestCreate_201(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	outbox.EXPECT().CreateWithURL(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	cache.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	auth := &fakeAuthClient{userID: uuid.New().String()}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	body := `{"long_url":"https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")

	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusCreated {
		t.Errorf("want 201, got %d: %s", rw.Code, rw.Body.String())
	}

	var resp dto.CreateURLResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ShortCode) != 7 {
		t.Errorf("expected short_code length 7, got %d", len(resp.ShortCode))
	}
}

func TestCreate_401_NoToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	auth := &fakeAuthClient{err: errors.New("no token")}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(`{"long_url":"https://x.com"}`))
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rw.Code)
	}
}

func TestCreate_401_InvalidToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	auth := &fakeAuthClient{err: errors.New("invalid token")}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(`{"long_url":"https://x.com"}`))
	req.Header.Set("Authorization", "Bearer bad-token")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rw.Code)
	}
}

func TestRedirect_302(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	expires := time.Now().Add(time.Hour)
	url := &entity.URL{
		ID:        uuid.New(),
		ShortCode: "abc1234",
		LongURL:   "https://example.com/long",
		Status:    entity.StatusActive,
		ExpiresAt: &expires,
	}
	cache.EXPECT().Get(gomock.Any(), "abc1234").Return(url, nil)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	auth := &fakeAuthClient{}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodGet, "/abc1234", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusFound {
		t.Errorf("want 302, got %d", rw.Code)
	}
	if loc := rw.Header().Get("Location"); loc != "https://example.com/long" {
		t.Errorf("unexpected Location: %s", loc)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestRedirect_404(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	cache.EXPECT().Get(gomock.Any(), "notfound").Return(nil, entity.ErrURLNotFound)
	urlRepo.EXPECT().FindByCode(gomock.Any(), "notfound").Return(nil, entity.ErrURLNotFound)

	auth := &fakeAuthClient{}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rw.Code)
	}
}

func TestRedirect_410_Expired(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	past := time.Now().Add(-time.Hour)
	url := &entity.URL{
		ShortCode: "expired",
		LongURL:   "https://old.com",
		Status:    entity.StatusActive,
		ExpiresAt: &past,
	}
	cache.EXPECT().Get(gomock.Any(), "expired").Return(url, nil)

	auth := &fakeAuthClient{}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodGet, "/expired", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusGone {
		t.Errorf("want 410, got %d", rw.Code)
	}
}

func TestBatchDelete_200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	userID := uuid.New()
	codes := []string{"aaa1111", "bbb2222"}

	urlRepo.EXPECT().SoftDeleteBatch(gomock.Any(), codes, userID).Return(int64(2), nil)
	cache.EXPECT().DeleteBatch(gomock.Any(), codes).Return(nil)
	outbox.EXPECT().AppendEvent(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	auth := &fakeAuthClient{userID: userID.String()}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	body, _ := json.Marshal(dto.BatchDeleteRequest{Codes: codes})
	req := httptest.NewRequest(http.MethodDelete, "/urls", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("Content-Type", "application/json")

	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", rw.Code, rw.Body.String())
	}

	var resp dto.BatchDeleteResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Deleted != 2 {
		t.Errorf("want deleted=2, got %d", resp.Deleted)
	}
	time.Sleep(10 * time.Millisecond)
}

func TestListURLs_200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)

	userID := uuid.New()
	urlRepo.EXPECT().FindByUserID(gomock.Any(), userID).Return([]*entity.URL{
		{
			ID: uuid.New(), UserID: userID, ShortCode: "abc1234",
			LongURL: "https://a.com", Status: entity.StatusActive,
			CreatedAt: time.Now(),
		},
	}, nil)

	auth := &fakeAuthClient{userID: userID.String()}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodGet, "/urls", nil)
	req.Header.Set("Authorization", "Bearer valid")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rw.Code)
	}

	var resp []dto.URLResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 1 {
		t.Errorf("want 1 url, got %d", len(resp))
	}
}

func TestHealthz_200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	urlRepo := mocks.NewMockURLRepository(ctrl)
	outbox := mocks.NewMockOutboxRepository(ctrl)
	cache := mocks.NewMockURLCache(ctrl)
	auth := &fakeAuthClient{}
	router := setupRouter(ctrl, urlRepo, outbox, cache, auth)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rw.Code)
	}
}
