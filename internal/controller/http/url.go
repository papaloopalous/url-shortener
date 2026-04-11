package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"shortener-service/internal/controller/http/dto"
	"shortener-service/internal/domain/entity"
	"shortener-service/internal/usecase"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type URLHandler struct {
	uc  *usecase.URLUsecase
	log *slog.Logger
}

func NewURLHandler(uc *usecase.URLUsecase, log *slog.Logger) *URLHandler {
	return &URLHandler{uc: uc, log: log}
}

func (h *URLHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req dto.CreateURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LongURL == "" {
		writeError(w, http.StatusBadRequest, "long_url is required")
		return
	}

	in := usecase.CreateInput{
		UserID:  userID,
		LongURL: req.LongURL,
	}
	if req.TTLDays != nil {
		d := time.Duration(*req.TTLDays) * 24 * time.Hour
		in.TTL = &d
	}

	out, err := h.uc.Create(r.Context(), in)
	if err != nil {
		h.log.ErrorContext(r.Context(), "create url failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, dto.CreateURLResponse{
		ShortCode: out.ShortCode,
		ShortURL:  out.ShortURL,
		LongURL:   out.LongURL,
		ExpiresAt: out.ExpiresAt,
	})
}

func (h *URLHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	longURL, err := h.uc.Redirect(r.Context(), usecase.RedirectInput{
		Code:      code,
		IP:        r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Referer:   r.Referer(),
	})
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrURLNotFound):
			writeError(w, http.StatusNotFound, "url not found")
		case errors.Is(err, entity.ErrURLExpired):
			writeError(w, http.StatusGone, "url expired")
		case errors.Is(err, entity.ErrURLDeleted):
			writeError(w, http.StatusGone, "url deleted")
		default:
			h.log.ErrorContext(r.Context(), "redirect failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	http.Redirect(w, r, longURL, http.StatusFound)
}

func (h *URLHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	urls, err := h.uc.ListByUser(r.Context(), userID)
	if err != nil {
		h.log.ErrorContext(r.Context(), "list urls failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]dto.URLResponse, 0, len(urls))
	for _, u := range urls {
		resp = append(resp, dto.URLResponse{
			ShortCode: u.ShortCode,
			LongURL:   u.LongURL,
			Status:    string(u.Status),
			ExpiresAt: u.ExpiresAt,
			CreatedAt: u.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *URLHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req dto.BatchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Codes) == 0 {
		writeError(w, http.StatusBadRequest, "codes must not be empty")
		return
	}

	n, err := h.uc.BatchDelete(r.Context(), usecase.BatchDeleteInput{
		Codes:   req.Codes,
		OwnerID: userID,
	})
	if err != nil {
		h.log.ErrorContext(r.Context(), "batch delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, dto.BatchDeleteResponse{Deleted: n})
}

func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, dto.ErrorResponse{Error: msg})
}

type contextKey string

const userIDKey contextKey = "user_id"

func UserIDFromCtx(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(userIDKey).(uuid.UUID)
	return v, ok
}

func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
