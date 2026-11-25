package intercaptor

import (
	"context"
	jwtTokens "eventix/pkg/jwt"
	"eventix/proto/auth/pb"
	"eventix/services/auth/internal/authctx"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func AuthInterceptor(secret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case pb.AuthService_GetMe_FullMethodName:
		case pb.AuthService_Register_FullMethodName,
			pb.AuthService_Login_FullMethodName,
			pb.AuthService_Logout_FullMethodName,
			pb.AuthService_RefreshToken_FullMethodName,
			pb.AuthService_ValidateToken_FullMethodName:
			return handler(ctx, req)
		default:
			return nil, status.Error(codes.PermissionDenied, "method is not allowed")
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}

		headers := md.Get("authorization")
		if len(headers) != 1 {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}

		parts := strings.Fields(headers[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header")
		}

		claims, err := jwtTokens.ValidateAccessToken(parts[1], secret)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil || userID == uuid.Nil {
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}

		ctx = authctx.WithUserID(ctx, userID)

		return handler(ctx, req)
	}
}
