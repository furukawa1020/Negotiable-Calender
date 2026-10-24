package request

import "time"

// A new mutation cannot predate the record it changes. Immutable replay checks
// run first so recovery does not depend on the caller's current clock.
func validMutationTime(value CoordinationRequest, now time.Time) bool {
	return !now.IsZero() && !now.Before(value.CreatedAt) && !now.Before(value.UpdatedAt)
}
