package graph

import (
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func MapGRPCToGraphQLError(err error) *gqlerror.Error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return &gqlerror.Error{
			Message: err.Error(),
			Extensions: map[string]interface{}{
				"code": "INTERNAL",
			},
		}
	}

	var code string
	var message string

	switch st.Code() {
	case codes.NotFound:
		code = "NOT_FOUND"
		message = st.Message()
	case codes.AlreadyExists:
		code = "CONFLICT"
		message = st.Message()
	case codes.InvalidArgument:
		code = "BAD_REQUEST"
		message = st.Message()
	case codes.Unauthenticated:
		code = "UNAUTHORIZED"
		message = st.Message()
	case codes.PermissionDenied:
		code = "FORBIDDEN"
		message = st.Message()
	case codes.Internal:
		code = "INTERNAL"
		message = "Internal server error"
	default:
		code = "UNKNOWN"
		message = st.Message()
	}

	return &gqlerror.Error{
		Message: message,
		Extensions: map[string]interface{}{
			"code":    code,
			"details": st.Message(),
		},
	}
}

func handleGRPCError(err error) error {
	if err == nil {
		return nil
	}
	return MapGRPCToGraphQLError(err)
}
