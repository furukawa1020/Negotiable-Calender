package firestorestore

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/accountcleanup"
)

// ListPendingAccountDeletions reads at most limit+1 marker documents, never users.
// Document-ID ordering is stable even when preceding markers become complete.
func (store *Auth) ListPendingAccountDeletions(ctx context.Context, limit int, cursor string) (accountcleanup.Page, error) {
	page := accountcleanup.Page{}
	if limit < 1 || limit > 100 {
		return page, accountcleanup.ErrInvalid
	}
	after, err := accountcleanup.DecodeCursor(cursor)
	if err != nil {
		return page, err
	}
	query := store.Client.Collection("accountDeletions").Where("Phase", "==", "deleting").
		OrderBy(firestore.DocumentID, firestore.Asc).Select("Phase").Limit(limit + 1)
	if after != "" {
		query = query.StartAfter(after)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return page, err
	}
	page.HasMore = len(docs) > limit
	if page.HasMore {
		docs = docs[:limit]
	}
	for _, doc := range docs {
		page.UserIDs = append(page.UserIDs, doc.Ref.ID)
	}
	return page, nil
}
