package tests

import (
	"context"
	jwtTokens "eventix/pkg/jwt"
	"eventix/proto/catalog/pb"
	"eventix/services/catalog/internal/catalogctx"
	"eventix/services/catalog/internal/interceptor"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor_AllowsPublicReads(t *testing.T) {
	check := interceptor.AuthInterceptor("test-secret")

	testCases := []struct {
		name   string
		method string
	}{
		{"GetEvent", pb.CatalogService_GetEvent_FullMethodName},
		{"ListEvents", pb.CatalogService_ListEvents_FullMethodName},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			info := &grpc.UnaryServerInfo{FullMethod: tc.method}

			resp, err := check(ctx, nil, info, func(ctx context.Context, _ any) (any, error) {
				_, hasUser := catalogctx.UserID(ctx)
				_, hasRole := catalogctx.UserRole(ctx)
				assert.False(t, hasUser)
				assert.False(t, hasRole)
				return "ok", nil
			})
			require.NoError(t, err)
			assert.Equal(t, "ok", resp)
		})
	}
}

func TestAuthInterceptor_PassesVerifiedIdentityToMutation(t *testing.T) {
	secret := "test-secret"
	check := interceptor.AuthInterceptor(secret)
	userID := uuid.New()

	testCases := []struct {
		name   string
		method string
		role   string
	}{
		{"CreateEvent as organizer", pb.CatalogService_CreateEvent_FullMethodName, "organizer"},
		{"UpdateEvent as user", pb.CatalogService_UpdateEvent_FullMethodName, "user"},
		{"DeleteEvent as admin", pb.CatalogService_DeleteEvent_FullMethodName, "admin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwtTokens.CreateAccessToken(userID, "test@example.com", tc.role, secret, time.Minute)
			require.NoError(t, err)

			md := metadata.Pairs("authorization", "Bearer "+token, "x-user-id", uuid.NewString(), "x-user-role", "other-role")
			ctx := metadata.NewIncomingContext(context.Background(), md)
			info := &grpc.UnaryServerInfo{FullMethod: tc.method}

			resp, err := check(ctx, nil, info, func(ctx context.Context, _ any) (any, error) {
				id, ok := catalogctx.UserID(ctx)
				require.True(t, ok)
				assert.Equal(t, userID, id)
				role, ok := catalogctx.UserRole(ctx)
				require.True(t, ok)
				assert.Equal(t, tc.role, role)
				return "ok", nil
			})
			require.NoError(t, err)
			assert.Equal(t, "ok", resp)
		})
	}
}

func TestAuthInterceptor_DeniesMutationWithoutValidAccessToken(t *testing.T) {
	secret := "test-secret"
	check := interceptor.AuthInterceptor(secret)
	userID := uuid.New()

	expired, err := jwtTokens.CreateAccessToken(userID, "test@example.com", "admin", secret, -time.Minute)
	require.NoError(t, err)
	refresh, err := jwtTokens.CreateRefreshToken(userID, uuid.NewString(), secret, time.Minute)
	require.NoError(t, err)

	testCases := []struct {
		name string
		md   metadata.MD
	}{
		{"No metadata", nil},
		{"Metadata without Bearer", metadata.Pairs("x-user-id", userID.String(), "x-user-role", "admin")},
		{"Malformed header", metadata.Pairs("authorization", "Bearer")},
		{"Invalid token", metadata.Pairs("authorization", "Bearer invalid")},
		{"Expired access token", metadata.Pairs("authorization", "Bearer "+expired)},
		{"Refresh token", metadata.Pairs("authorization", "Bearer "+refresh)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.md != nil {
				ctx = metadata.NewIncomingContext(ctx, tc.md)
			}
			info := &grpc.UnaryServerInfo{FullMethod: pb.CatalogService_CreateEvent_FullMethodName}

			resp, err := check(ctx, nil, info, func(context.Context, any) (any, error) {
				t.Fatal("handler should not be called")
				return nil, nil
			})
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestAuthInterceptor_DeniesUnknownMethod(t *testing.T) {
	check := interceptor.AuthInterceptor("test-secret")
	info := &grpc.UnaryServerInfo{FullMethod: "/catalog.CatalogService/Unknown"}

	resp, err := check(context.Background(), nil, info, func(context.Context, any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
