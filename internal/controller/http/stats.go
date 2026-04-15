package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"analytics-service/internal/controller/http/dto"
	"analytics-service/internal/domain/entity"
	"analytics-service/internal/usecase"
	"errors"

	"github.com/go-chi/chi/v5"
)

type StatsHandler struct {
	uc  *usecase.StatsUsecase
	log *slog.Logger
}

func NewStatsHandler(uc *usecase.StatsUsecase, log *slog.Logger) *StatsHandler {
	return &StatsHandler{uc: uc, log: log}
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	stats, err := h.uc.GetStats(r.Context(), code)
	if err != nil {
		if errors.Is(err, entity.ErrStatsNotFound) {
			writeError(w, http.StatusNotFound, "stats not found")
			return
		}
		h.log.ErrorContext(r.Context(), "get stats failed", "error", err, "code", code)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := dto.StatsResponse{
		ShortCode:   stats.ShortCode,
		TotalClicks: stats.TotalClicks,
		UniqueIPs:   stats.UniqueIPs,
		LastClickAt: stats.LastClickAt,
	}
	for _, c := range stats.TopCountries {
		resp.TopCountries = append(resp.TopCountries, dto.CountryStat{
			Country: c.Country,
			Clicks:  c.Clicks,
		})
	}

	writeJSON(w, http.StatusOK, resp)
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
