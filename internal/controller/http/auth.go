package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"auth-service/internal/controller/http/dto"
	"auth-service/internal/domain/entity"
	"auth-service/internal/usecase"
)

type AuthController struct {
	uc *usecase.AuthUsecase
}

func NewAuthController(uc *usecase.AuthUsecase) *AuthController {
	return &AuthController{uc: uc}
}

// POST /auth/register
func (c *AuthController) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Validate(); err != nil {
		respondErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pair, err := c.uc.Register(
		r.Context(),
		usecase.RegisterInput{Email: req.Email, Password: req.Password},
		r.UserAgent(), r.RemoteAddr,
	)
	switch {
	case errors.Is(err, entity.ErrUserAlreadyExists):
		respondErr(w, http.StatusConflict, "email already registered")
	case err != nil:
		respondErr(w, http.StatusInternalServerError, "internal error")
	default:
		respond(w, http.StatusCreated, toTokenResponse(pair))
	}
}

// POST /auth/login
func (c *AuthController) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Validate(); err != nil {
		respondErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pair, err := c.uc.Login(
		r.Context(),
		usecase.LoginInput{Email: req.Email, Password: req.Password},
		r.UserAgent(), r.RemoteAddr,
	)
	switch {
	case errors.Is(err, entity.ErrInvalidPassword):
		respondErr(w, http.StatusUnauthorized, "invalid credentials")
	case err != nil:
		respondErr(w, http.StatusInternalServerError, "internal error")
	default:
		respond(w, http.StatusOK, toTokenResponse(pair))
	}
}

// POST /auth/refresh
func (c *AuthController) Refresh(w http.ResponseWriter, r *http.Request) {
	var req dto.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Validate(); err != nil {
		respondErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pair, err := c.uc.Refresh(r.Context(), req.RefreshToken, r.UserAgent(), r.RemoteAddr)
	switch {
	case errors.Is(err, entity.ErrTokenReuse):
		respondErr(w, http.StatusUnauthorized, "session invalidated")
	case errors.Is(err, entity.ErrSessionNotFound),
		errors.Is(err, entity.ErrSessionExpired),
		errors.Is(err, entity.ErrSessionRevoked):
		respondErr(w, http.StatusUnauthorized, "session expired or not found")
	case err != nil:
		respondErr(w, http.StatusInternalServerError, "internal error")
	default:
		respond(w, http.StatusOK, toTokenResponse(pair))
	}
}

// POST /auth/logout
func (c *AuthController) Logout(w http.ResponseWriter, r *http.Request) {
	var req dto.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_ = c.uc.Logout(
		r.Context(),
		req.RefreshToken,
		req.JTI,
		time.Duration(req.AccessTTLSeconds)*time.Second,
	)
	w.WriteHeader(http.StatusNoContent)
}

func toTokenResponse(p *usecase.TokenPair) dto.TokenResponse {
	return dto.TokenResponse{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		ExpiresIn:    int(p.ExpiresIn.Seconds()),
		TokenType:    "Bearer",
	}
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func respondErr(w http.ResponseWriter, status int, msg string) {
	respond(w, status, dto.ErrorResponse{Error: msg})
}
