package firestorestore

import (
	"context"
	"errors"
	"time"

	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	request "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

const planningProjectionLimit = 256
const planningBookingRoleLimit = 64

var errPlanningLimit = errors.New("planning source read limit exceeded")

// LoadPlanningSources never falls back to unbounded List methods. Each query
// has a server-side limit plus one overflow sentinel; overflow discards ALL data.
// It is not a reservation. Explicit acceptance remains transactional.
func (store *Request) LoadPlanningSources(ctx context.Context, target, requester string, from, to time.Time) ([]projection.ScheduleProjection, []request.CoordinationRequest, error) {
	if target == "" || requester == "" || target == requester || !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		return nil, nil, calendarintegration.ErrSourceUnavailable
	}
	guard := func() error {
		for _, user := range []string{target, requester} {
			if deleting, err := store.accountIsDeleting(ctx, user); err != nil {
				return err
			} else if deleting {
				return errAccountDeleting
			}
			if _, err := store.Client.Collection("users").Doc(user).Get(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	if err := guard(); err != nil {
		return nil, nil, err
	}
	var source calendarintegration.SourceState
	revision, ready, err := store.projectionReadRevision(ctx, target, &source)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, nil, calendarintegration.ErrSourceUnavailable
	}
	// One range field uses the automatic EndAt index. Do not filter on expiry:
	// stale/contradictory rows must remain visible to the availability validator.
	// Future rows outside the upper bound may conservatively exhaust the budget.
	q := store.Client.Collection("users").Doc(target).Collection("scheduleProjections").Where("EndAt", ">", from).Limit(planningProjectionLimit + 1)
	documents, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, nil, err
	}
	if len(documents) > planningProjectionLimit {
		return nil, nil, errPlanningLimit
	}
	segments := make([]projection.ScheduleProjection, 0, len(documents))
	for _, doc := range documents {
		var value projection.ScheduleProjection
		if err := doc.DataTo(&value); err != nil {
			return nil, nil, err
		}
		if value.Validate() != nil || value.UserID != target {
			return nil, nil, calendarintegration.ErrSourceUnavailable
		}
		if value.StartAt.Before(to) {
			segments = append(segments, value)
		}
	}
	bookings := []request.CoordinationRequest{}
	// Do not restrict organization or dates: cross-workspace and malformed
	// accepted records also participate in the existing fail-closed validator.
	for _, user := range []string{target, requester} {
		for _, role := range []string{"TargetUserID", "RequesterUserID"} {
			q := store.Client.Collection("coordinationRequests").Where(role, "==", user).Where("Status", "==", request.Accepted).Limit(planningBookingRoleLimit + 1)
			documents, err := q.Documents(ctx).GetAll()
			if err != nil {
				return nil, nil, err
			}
			if len(documents) > planningBookingRoleLimit {
				return nil, nil, errPlanningLimit
			}
			for _, doc := range documents {
				var value request.CoordinationRequest
				if err := doc.DataTo(&value); err != nil {
					return nil, nil, err
				}
				bookings = append(bookings, value)
			}
		}
	}
	current, ready, err := store.projectionReadRevision(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	if !ready || current != revision {
		return nil, nil, calendarintegration.ErrSourceUnavailable
	}
	if err := guard(); err != nil {
		return nil, nil, err
	}
	return projection.BoundToSource(segments, source), bookings, nil
}

// Equality-only actor+status queries use automatic single-field index merging.
// There are no offsets, pagination loops, counts, or application retries here.
// Maximum source document results: 257 + 4*65 = 517 (including sentinels).
