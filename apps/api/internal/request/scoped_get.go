package request

import "context"

// ScopedGetStore authorizes public detail/export reads in the same snapshot as
// the request and its options. Global reads remain for internal reconciliation.
type ScopedGetStore interface {
	GetInOrganization(context.Context, string, string, string) (CoordinationRequest, error)
}

func (store *PostgresStore) GetInOrganization(ctx context.Context, id, actor, org string) (CoordinationRequest, error) {
	if actor == "" || org == "" {
		return CoordinationRequest{}, ErrCreationForbidden
	}
	return store.getForUser(ctx, id, actor, org)
}

var _ ScopedGetStore = (*PostgresStore)(nil)
