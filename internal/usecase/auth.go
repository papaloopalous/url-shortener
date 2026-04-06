package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/crypto/bcrypt"

	"auth-service/internal/domain/entity"
	"auth-service/internal/domain/service"
	"auth-service/pkg/metrics"
	"auth-service/pkg/tracing"
)

var tracer = tracing.Tracer("usecase/auth")

type JWTManager struct {
	secret string
}

func NewJWTManager(secret string) *JWTManager {
	return &JWTManager{secret: secret}
}

type claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func (m *JWTManager) GenerateAccess(userID string, ttl time.Duration) (string, string, error) {
	jti := uuid.New().String()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	signed, err := tok.SignedString([]byte(m.secret))
	if err != nil {
		return "", "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, jti, nil
}

func (m *JWTManager) VerifyAccess(tokenStr string) (string, string, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(m.secret), nil
	})
	if err != nil || !tok.Valid {
		return "", "", entity.ErrInvalidPassword
	}
	c, ok := tok.Claims.(*claims)
	if !ok {
		return "", "", fmt.Errorf("unexpected claims type")
	}
	return c.UserID, c.ID, nil
}

type AuthUsecase struct {
	users      service.UserRepository
	sessions   service.SessionRepository
	cache      service.TokenCache
	tokenMgr   service.TokenManager
	accessTTL  time.Duration
	refreshTTL time.Duration
	bcryptCost int
}

func NewAuthUsecase(
	users service.UserRepository,
	sessions service.SessionRepository,
	cache service.TokenCache,
	tokenMgr service.TokenManager,
	accessTTL, refreshTTL time.Duration,
	bcryptCost int,
) *AuthUsecase {
	return &AuthUsecase{
		users:      users,
		sessions:   sessions,
		cache:      cache,
		tokenMgr:   tokenMgr,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		bcryptCost: bcryptCost,
	}
}

func (uc *AuthUsecase) Register(inp RegisterInput, ua, ip string) (*TokenPair, error) {
	ctx, span := tracer.Start(context.Background(), "AuthUsecase.Register")
	defer span.End()
	span.SetAttributes(attribute.String("user.email", inp.Email))

	if _, err := uc.users.FindByEmail(inp.Email); err == nil {
		span.SetStatus(codes.Error, "duplicate email")
		metrics.IncEvent("register_duplicate")
		return nil, entity.ErrUserAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(inp.Password), uc.bcryptCost)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &entity.User{
		ID:           uuid.New(),
		Email:        inp.Email,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := uc.users.Create(user); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("create user: %w", err)
	}

	pair, err := uc.issueTokenPair(ctx, user.ID, ua, ip)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	metrics.IncEvent("register_ok")
	return pair, nil
}

func (uc *AuthUsecase) Login(inp LoginInput, ua, ip string) (*TokenPair, error) {
	ctx, span := tracer.Start(context.Background(), "AuthUsecase.Login")
	defer span.End()
	span.SetAttributes(attribute.String("user.email", inp.Email))

	user, err := uc.users.FindByEmail(inp.Email)
	if err != nil {
		// Return generic error to prevent user enumeration.
		metrics.IncEvent("login_fail")
		return nil, entity.ErrInvalidPassword
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(inp.Password)); err != nil {
		metrics.IncEvent("login_fail")
		return nil, entity.ErrInvalidPassword
	}

	pair, err := uc.issueTokenPair(ctx, user.ID, ua, ip)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.String("user.id", user.ID.String()))
	span.SetStatus(codes.Ok, "")
	metrics.IncEvent("login_ok")
	return pair, nil
}

func (uc *AuthUsecase) Refresh(rawToken, ua, ip string) (*TokenPair, error) {
	ctx, span := tracer.Start(context.Background(), "AuthUsecase.Refresh")
	defer span.End()

	hash, err := uc.hashToken(rawToken)
	if err != nil {
		return nil, fmt.Errorf("hash token: %w", err)
	}

	session, err := uc.sessions.FindByTokenHash(hash)
	if err != nil {
		return nil, entity.ErrSessionNotFound
	}

	span.SetAttributes(
		attribute.String("user.id", session.UserID.String()),
		attribute.String("session.id", session.ID.String()),
	)

	if session.IsRevoked() {
		_ = uc.sessions.RevokeAllByUserID(session.UserID)
		span.SetStatus(codes.Error, "token reuse")
		metrics.IncEvent("token_reuse")
		return nil, entity.ErrTokenReuse
	}

	if err := session.Validate(); err != nil {
		return nil, err
	}

	if err := uc.sessions.RevokeByID(session.ID); err != nil {
		return nil, fmt.Errorf("revoke old session: %w", err)
	}

	pair, err := uc.issueTokenPair(ctx, session.UserID, ua, ip)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	metrics.IncEvent("refresh_ok")
	return pair, nil
}

func (uc *AuthUsecase) Logout(rawToken, jti string, accessTTLLeft time.Duration) error {
	_, span := tracer.Start(context.Background(), "AuthUsecase.Logout")
	defer span.End()

	hash, err := uc.hashToken(rawToken)
	if err != nil {
		return fmt.Errorf("hash token: %w", err)
	}

	if session, err := uc.sessions.FindByTokenHash(hash); err == nil {
		_ = uc.sessions.RevokeByID(session.ID)
	}

	if jti != "" && accessTTLLeft > 0 {
		if err := uc.cache.RevokeJTI(jti, accessTTLLeft); err != nil {
			return fmt.Errorf("revoke jti: %w", err)
		}
	}

	span.SetStatus(codes.Ok, "")
	metrics.IncEvent("logout")
	return nil
}

func (uc *AuthUsecase) issueTokenPair(ctx context.Context, userID uuid.UUID, ua, ip string) (*TokenPair, error) {
	_, span := tracer.Start(ctx, "AuthUsecase.issueTokenPair")
	defer span.End()

	accessToken, _, err := uc.tokenMgr.GenerateAccess(userID.String(), uc.accessTTL)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	rawRefresh := uuid.New().String()
	refreshHash, err := uc.hashToken(rawRefresh)
	if err != nil {
		return nil, fmt.Errorf("hash refresh token: %w", err)
	}

	session := &entity.RefreshSession{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: refreshHash,
		UserAgent: ua,
		IPAddress: ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(uc.refreshTTL),
	}
	if err := uc.sessions.Create(session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    uc.accessTTL,
	}, nil
}

func (uc *AuthUsecase) hashToken(raw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}
