package request

import "context"

// ScopedListStore serves workspace inbox/sent views. Global Store reads remain
// available for cross-workspace conflict detection and personal export only.
type ScopedListStore interface {
	ListInOrganization(ctx context.Context, actor, organizationID string, sent bool) ([]CoordinationRequest, error)
}

func (store *PostgresStore) ListInOrganization(ctx context.Context, actor, org string, sent bool) ([]CoordinationRequest, error) {
	if actor == "" || org == "" {
		return nil, ErrCreationForbidden
	}
	return store.listForUser(ctx, actor, !sent, sent, org)
}

var _ ScopedListStore = (*PostgresStore)(nil)
