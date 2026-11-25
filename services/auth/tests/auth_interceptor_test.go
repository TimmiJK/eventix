package tests

import (
	"context"
	jwtTokens "eventix/pkg/jwt"
	"eventix/proto/auth/pb"
	"eventix/services/auth/internal/authctx"
	interceptor "eventix/services/auth/internal/interceptor"
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

func TestAuthInterceptor_GetMe(t *testing.T) {
	secret := "test-secret"
	userID := uuid.New()
	access, err := jwtTokens.CreateAccessToken(userID, "test@example.com", "user", secret, time.Minute)
	require.NoError(t, err)
	expired, err := jwtTokens.CreateAccessToken(userID, "test@example.com", "user", secret, -time.Minute)
	require.NoError(t, err)
	refresh, err := jwtTokens.CreateRefreshToken(userID, uuid.NewString(), secret, time.Minute)
	require.NoError(t, err)
	wrongSecret, err := jwtTokens.CreateAccessToken(userID, "test@example.com", "user", "other-secret", time.Minute)
	require.NoError(t, err)

	check := interceptor.AuthInterceptor(secret)
	info := &grpc.UnaryServerInfo{FullMethod: pb.AuthService_GetMe_FullMethodName}
	req := &pb.GetMeRequest{}

	t.Run("Success", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "bearer "+access))
		expected := &pb.GetMeResponse{UserId: userID.String()}

		resp, err := check(ctx, req, info, func(ctx context.Context, input any) (any, error) {
			id, ok := authctx.UserID(ctx)
			require.True(t, ok)
			assert.Equal(t, userID, id)
			assert.Same(t, req, input)
			return expected, nil
		})
		require.NoError(t, err)
		assert.Same(t, expected, resp)
	})

	cases := []struct {
		name    string
		headers []string
	}{
		{"MissingMetadata", nil},
		{"WrongScheme", []string{"Basic " + access}},
		{"DuplicateAuthorization", []string{"Bearer " + access, "Bearer " + access}},
		{"MalformedToken", []string{"Bearer invalid"}},
		{"ExpiredAccessToken", []string{"Bearer " + expired}},
		{"WrongSecret", []string{"Bearer " + wrongSecret}},
		{"RefreshToken", []string{"Bearer " + refresh}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.headers != nil {
				ctx = metadata.NewIncomingContext(ctx, metadata.MD{"authorization": tc.headers})
			}
			resp, err := check(ctx, req, info, func(context.Context, any) (any, error) {
				t.Fatal("handler called without valid access token")
				return nil, nil
			})
			assert.Nil(t, resp)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestAuthInterceptor_MethodPolicy(t *testing.T) {
	check := interceptor.AuthInterceptor("test-secret")

	t.Run("RefreshDoesNotRequireAccessToken", func(t *testing.T) {
		req := &pb.RefreshTokenRequest{}
		info := &grpc.UnaryServerInfo{FullMethod: pb.AuthService_RefreshToken_FullMethodName}
		expected := &pb.RefreshTokenResponse{}
		resp, err := check(context.Background(), req, info, func(ctx context.Context, input any) (any, error) {
			_, authenticated := authctx.UserID(ctx)
			assert.False(t, authenticated)
			assert.Same(t, req, input)
			return expected, nil
		})
		require.NoError(t, err)
		assert.Same(t, expected, resp)
	})

	t.Run("UnknownMethod", func(t *testing.T) {
		info := &grpc.UnaryServerInfo{FullMethod: "/auth.AuthService/Unknown"}
		resp, err := check(context.Background(), nil, info, func(context.Context, any) (any, error) {
			t.Fatal("handler called for unknown method")
			return nil, nil
		})
		assert.Nil(t, resp)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}
