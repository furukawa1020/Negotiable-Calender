package httpapi

import (
	"context"
	"errors"
)

// Error strings can embed SQL values, document paths, user text or credentials.
// Log only a static operation label plus this allowlisted classification.
func safeFailureCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "operation_failed"
	}
}
