package catalogctx

import (
	"context"

	"github.com/google/uuid"
)

type userIDKey struct{}
type userRoleKey struct{}

func WithUserIDAndRole(ctx context.Context, id uuid.UUID, role string) context.Context {
	ctx = context.WithValue(ctx, userIDKey{}, id)
	ctx = context.WithValue(ctx, userRoleKey{}, role)
	return ctx
}

func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

func UserRole(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(userRoleKey{}).(string)
	return role, ok && role != ""
}
