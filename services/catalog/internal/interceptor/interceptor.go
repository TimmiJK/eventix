package interceptor

import (
	"context"
	jwtTokens "eventix/pkg/jwt"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/catalogctx"
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
		case pb.CatalogService_CreateEvent_FullMethodName, pb.CatalogService_UpdateEvent_FullMethodName, pb.CatalogService_DeleteEvent_FullMethodName:
		case pb.CatalogService_GetEvent_FullMethodName, pb.CatalogService_ListEvents_FullMethodName:
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
		role := claims.Role
		if err != nil || userID == uuid.Nil || role == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}

		ctx = catalogctx.WithUserIDAndRole(ctx, userID, role)

		return handler(ctx, req)
	}
}
