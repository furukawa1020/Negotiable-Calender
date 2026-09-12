package firestorestore

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
)

var errAccountDeleting = errors.New("account deletion has started")

type accountDeletion struct {
	Phase           string
	StartedAt       time.Time
	OrganizationIDs []string
}

func (b *Backend) accountDeletionRef(userID string) *firestore.DocumentRef {
	return b.Client.Collection("accountDeletions").Doc(userID)
}

func (b *Backend) guardAccountActive(ctx context.Context, tx *firestore.Transaction, userID string) error {
	_, err := tx.Get(b.accountDeletionRef(userID))
	if firestoreNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return errAccountDeleting
}

func (b *Backend) accountIsDeleting(ctx context.Context, userID string) (bool, error) {
	_, err := b.accountDeletionRef(userID).Get(ctx)
	if firestoreNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Checking authoritative membership and marking deletion must be atomic. Other
// deleting owners do not count as successors, including concurrent deletions.
func (store *Auth) beginAccountDeletion(ctx context.Context, userID string) (accountDeletion, error) {
	var value accountDeletion
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		value = accountDeletion{}
		doc, err := tx.Get(store.accountDeletionRef(userID))
		if err == nil {
			if err := doc.DataTo(&value); err != nil {
				return err
			}
			if value.Phase != "deleting" && value.Phase != "complete" {
				return errAccountDeleting
			}
			return nil
		}
		if !firestoreNotFound(err) {
			return err
		}
		if _, err := tx.Get(store.Client.Collection("users").Doc(userID)); firestoreNotFound(err) {
			return auth.ErrNotFound
		} else if err != nil {
			return err
		}
		orgs, err := tx.Documents(store.Client.Collection("organizations")).GetAll()
		if err != nil {
			return err
		}
		for _, org := range orgs {
			memberDoc, err := tx.Get(org.Ref.Collection("members").Doc(userID))
			if firestoreNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			var membership membershipRecord
			if err := memberDoc.DataTo(&membership); err != nil {
				return err
			}
			if !membership.Role.Valid() {
				return organization.ErrForbidden
			}
			value.OrganizationIDs = append(value.OrganizationIDs, org.Ref.ID)
			if membership.Role != organization.Owner {
				continue
			}
			members, err := tx.Documents(org.Ref.Collection("members")).GetAll()
			if err != nil {
				return err
			}
			successors := 0
			for _, member := range members {
				if member.Ref.ID == userID {
					continue
				}
				var candidate membershipRecord
				if err := member.DataTo(&candidate); err != nil {
					return err
				}
				if candidate.Role != organization.Owner {
					continue
				}
				_, err := tx.Get(store.accountDeletionRef(member.Ref.ID))
				if firestoreNotFound(err) {
					successors++
				} else if err != nil {
					return err
				}
			}
			if len(members) > 1 && successors == 0 {
				return auth.ErrLastOrganizationOwner
			}
		}
		value.Phase = "deleting"
		value.StartedAt = time.Now().UTC()
		if err := tx.Set(store.accountDeletionRef(userID), value); err != nil {
			return err
		}
		return tx.Delete(store.Client.Collection("calendarConnections").Doc(userID))
	})
	return value, err
}

// ResumeAccountDeletion cannot initiate a new deletion. It is intended for an
// authenticated operator after an interrupted, already-authorized cleanup.
func (store *Auth) ResumeAccountDeletion(ctx context.Context, userID string) error {
	doc, err := store.accountDeletionRef(userID).Get(ctx)
	if err != nil {
		return err
	}
	var value accountDeletion
	if err := doc.DataTo(&value); err != nil {
		return err
	}
	if value.Phase == "complete" {
		return nil
	}
	if value.Phase != "deleting" {
		return errAccountDeleting
	}
	return store.DeleteAccount(ctx, userID)
}
