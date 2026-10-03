package firestorestore

import (
	"context"

	"cloud.google.com/go/firestore"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (store *Request) LoadConfirmedRanges(ctx context.Context, requester, target string) ([]coord.ReservedRange, error) {
	if requester == "" || target == "" || requester == target {
		return nil, coord.ErrReservationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, coord.ReservationReadTimeout)
	defer cancel()
	var ranges []coord.ReservedRange
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ranges = nil
		values := []coord.CoordinationRequest{}
		seen := map[string]bool{}
		for _, participant := range []string{requester, target} {
			if err := store.guardAccountActive(ctx, tx, participant); err != nil {
				return err
			}
			if _, err := tx.Get(store.Client.Collection("users").Doc(participant)); err != nil {
				return err
			}
			for _, role := range []string{"RequesterUserID", "TargetUserID"} {
				query := store.acceptedReservationQuery(participant, role).
					Select("ID", "Status", "RequesterUserID", "TargetUserID", "AcceptedOptionID", "Options").
					Limit(coord.ReservationRoleLimit + 1)
				docs, err := tx.Documents(query).GetAll()
				if err != nil {
					return err
				}
				if len(docs) > coord.ReservationRoleLimit {
					return coord.ErrReservationUnavailable
				}
				for _, doc := range docs {
					var value coord.CoordinationRequest
					if err := doc.DataTo(&value); err != nil {
						return err
					}
					if value.ID != doc.Ref.ID || value.Status != coord.Accepted ||
						(role == "RequesterUserID" && value.RequesterUserID != participant) ||
						(role == "TargetUserID" && value.TargetUserID != participant) {
						return coord.ErrReservationUnavailable
					}
					if !seen[value.ID] {
						seen[value.ID] = true
						values = append(values, value)
					}
				}
			}
		}
		var err error
		ranges, err = coord.ReservationRanges(values)
		return err
	}, firestore.ReadOnly)
	if err != nil {
		return nil, err
	}
	return ranges, nil
}

var _ coord.ReservationStore = (*Request)(nil)
