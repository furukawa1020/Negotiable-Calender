package calendar

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Never return error text, provider metadata, index creation URLs or identifiers.
// FailedPrecondition is a category, not proof that an index is missing.
func claimCauseCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	switch status.Code(err) {
	case codes.FailedPrecondition:
		return "failed_precondition"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	case codes.Unavailable:
		return "unavailable"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	case codes.Canceled:
		return "canceled"
	case codes.NotFound:
		return "not_found"
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.Aborted:
		return "aborted"
	case codes.Internal:
		return "internal"
	default:
		return "unknown"
	}
}
