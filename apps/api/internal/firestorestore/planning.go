package firestorestore

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
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
	ctx, cancel := context.WithTimeout(ctx, request.ReservationReadTimeout)
	defer cancel()
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
	// Bound reads to the requested interval, preserving stale/contradictory evidence.
	q := store.availabilityOverlapQuery(target, from, to).Limit(planningProjectionLimit + 1)
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
		if value.Validate() != nil || value.UserID != target || value.ID != doc.Ref.ID {
			return nil, nil, calendarintegration.ErrSourceUnavailable
		}
		if value.StartAt.Before(to) {
			segments = append(segments, value)
		}
	}
	bookings, err := store.planningBookings(ctx, target, requester)
	if err != nil {
		return nil, nil, err
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

func (store *Request) planningBookings(ctx context.Context, target, requester string) ([]request.CoordinationRequest, error) {
	var bookings []request.CoordinationRequest
	// Do not restrict organization or dates: cross-workspace and malformed
	// accepted records must not disappear from conflict evidence. All four
	// queries use one read-only snapshot; this still does not reserve a slot.
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		bookings = []request.CoordinationRequest{}
		seen := map[string]bool{}
		for _, user := range []string{target, requester} {
			for _, role := range []string{"TargetUserID", "RequesterUserID"} {
				q := store.acceptedReservationQuery(user, role).
					Select("ID", "Status", "RequesterUserID", "TargetUserID", "AcceptedOptionID", "Options").
					Limit(planningBookingRoleLimit + 1)
				documents, err := tx.Documents(q).GetAll()
				if err != nil {
					return err
				}
				if len(documents) > planningBookingRoleLimit {
					return errPlanningLimit
				}
				for _, doc := range documents {
					var value request.CoordinationRequest
					if err := doc.DataTo(&value); err != nil {
						return err
					}
					if value.ID != doc.Ref.ID || value.Status != request.Accepted ||
						(role == "TargetUserID" && value.TargetUserID != user) ||
						(role == "RequesterUserID" && value.RequesterUserID != user) {
						return calendarintegration.ErrSourceUnavailable
					}
					if !seen[value.ID] {
						seen[value.ID] = true
						bookings = append(bookings, value)
					}
				}
			}
		}
		_, err := request.ReservationRanges(bookings)
		return err
	}, firestore.ReadOnly)
	if err != nil {
		return nil, err
	}
	return bookings, nil
}

// Equality-only actor+status queries use automatic single-field index merging.
// There are no offsets, pagination loops, counts, or application retries here.
// Maximum source document results: 257 + 4*65 = 517 (including sentinels).
