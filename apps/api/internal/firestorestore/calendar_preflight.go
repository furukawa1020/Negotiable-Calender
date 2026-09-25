package firestorestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// Share predicates and ordering with claims so the startup probe cannot silently
// validate a simpler query that does not require the production sync index.
func (store *Calendar) legacyDueQuery() firestore.Query {
	return store.Client.Collection("calendarConnections").Where("ReconnectRequired", "==", false).Where("NextAttemptAt", "==", nil)
}

func (store *Calendar) datedDueQuery(now time.Time) firestore.Query {
	return store.Client.Collection("calendarConnections").Where("ReconnectRequired", "==", false).Where("NextAttemptAt", "<=", now).OrderBy("NextAttemptAt", firestore.Asc)
}

// CheckSyncQueries is read-only: no connection decoding, leases, transactions,
// token access or Google calls. It retrieves at most two document references.
func (store *Calendar) CheckSyncQueries(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, query := range []firestore.Query{store.legacyDueQuery(), store.datedDueQuery(time.Now().UTC())} {
		if _, err := query.Select().Limit(1).Documents(ctx).GetAll(); err != nil {
			return err
		}
	}
	return nil
}
