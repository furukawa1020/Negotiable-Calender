package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

// Include every overlap, including long/spanning, stale and contradictory rows.
// Do not approximate overlap using a fixed bucket length or filter by state/expiry.
// The matching COLLECTION composite index is checked in to firestore.indexes.json.
func (b *Backend) availabilityOverlapQuery(userID string, from, to time.Time) firestore.Query {
	return b.Client.Collection("users").Doc(userID).Collection("scheduleProjections").
		Where("StartAt", "<", to).Where("EndAt", ">", from).
		OrderBy("StartAt", firestore.Asc).OrderBy("EndAt", firestore.Asc)
}

// Share the exact reservation predicates between planning, transactional booking,
// and preflight. Do not add an organization/date filter: malformed accepted
// bookings and cross-workspace conflicts must remain visible to validation.
func (b *Backend) acceptedReservationQuery(participant, role string) firestore.Query {
	return b.Client.Collection("coordinationRequests").
		Where(role, "==", participant).Where("Status", "==", coord.Accepted)
}

// CheckAvailabilityQueries verifies the actual production queries before serving
// traffic. It reads at most three document references for a synthetic participant;
// it never decodes data, writes records, or touches calendar tokens.
func (store *Request) CheckAvailabilityQueries(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	from := time.Unix(0, 0).UTC()
	const participant = "_availability-index-probe"
	queries := []firestore.Query{
		store.availabilityOverlapQuery(participant, from, from.Add(time.Hour)),
		store.acceptedReservationQuery(participant, "RequesterUserID"),
		store.acceptedReservationQuery(participant, "TargetUserID"),
	}
	for _, query := range queries {
		if _, err := query.Select().Limit(1).Documents(ctx).GetAll(); err != nil {
			return err
		}
	}
	return nil
}
