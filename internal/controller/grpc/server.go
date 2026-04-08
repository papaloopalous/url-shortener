package grpc

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"auth-service/internal/domain/service"
	"auth-service/pkg/metrics"
	"auth-service/pkg/tracing"
	pb "auth-service/proto/auth"
)

var tracer = tracing.Tracer("controller/grpc")

type AuthGRPCServer struct {
	pb.UnimplementedAuthServiceServer
	tokenMgr   service.TokenManager
	tokenCache service.TokenCache
	log        *slog.Logger
}

func NewAuthGRPCServer(
	tokenMgr service.TokenManager,
	tokenCache service.TokenCache,
	log *slog.Logger,
) *AuthGRPCServer {
	return &AuthGRPCServer{tokenMgr: tokenMgr, tokenCache: tokenCache, log: log}
}

func (s *AuthGRPCServer) ValidateToken(
	ctx context.Context,
	req *pb.ValidateTokenRequest,
) (*pb.ValidateTokenResponse, error) {
	ctx, span := tracer.Start(ctx, "AuthGRPCServer.ValidateToken")
	defer span.End()

	userID, jti, err := s.tokenMgr.VerifyAccess(req.AccessToken)
	if err != nil {
		span.SetStatus(codes.Error, "invalid token")
		metrics.RecordGRPC("ValidateToken", "UNAUTHENTICATED", 0)
		return nil, status.Errorf(grpccodes.Unauthenticated, "invalid or expired token")
	}

	span.SetAttributes(
		attribute.String("user.id", userID),
		attribute.String("jti", jti),
	)

	revoked, err := s.tokenCache.IsRevoked(ctx, jti)
	if err != nil {
		s.log.WarnContext(ctx, "redis unavailable for jti check, failing open",
			"jti", jti, "err", err,
		)
	}
	if revoked {
		span.SetStatus(codes.Error, "token revoked")
		metrics.IncEvent("token_revoked_grpc")
		return nil, status.Errorf(grpccodes.Unauthenticated, "token has been revoked")
	}

	span.SetStatus(codes.Ok, "")
	metrics.IncEvent("token_valid_grpc")

	return &pb.ValidateTokenResponse{
		UserId: userID,
		Jti:    jti,
	}, nil
}
