package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// Include every overlap, including long/spanning, stale and contradictory rows.
// Do not approximate overlap using a fixed bucket length or filter by state/expiry.
// The matching COLLECTION composite index is checked in to firestore.indexes.json.
func (b *Backend) availabilityOverlapQuery(userID string, from, to time.Time) firestore.Query {
	return b.Client.Collection("users").Doc(userID).Collection("scheduleProjections").
		Where("StartAt", "<", to).Where("EndAt", ">", from).
		OrderBy("StartAt", firestore.Asc).OrderBy("EndAt", firestore.Asc)
}

// CheckAvailabilityQueries verifies the actual production index before serving
// traffic. It reads at most one document reference in a synthetic user namespace;
// it never decodes data, writes records, or touches calendar tokens.
func (store *Request) CheckAvailabilityQueries(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	from := time.Unix(0, 0).UTC()
	_, err := store.availabilityOverlapQuery("_availability-index-probe", from, from.Add(time.Hour)).
		Select().Limit(1).Documents(ctx).GetAll()
	return err
}
