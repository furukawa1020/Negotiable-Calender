package calendar

import (
	"context"
	"errors"
	"fmt"
)

func (handler *Handler) PrepareForAccountDeletion(ctx context.Context, userID string) (func(context.Context) error, error) {
	connection, err := handler.store.GetConnection(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load calendar grant for revocation: %w", err)
	}
	if handler.cipher == nil {
		return nil, fmt.Errorf("calendar token cipher is unavailable")
	}
	refreshToken, err := handler.cipher.Decrypt(connection.RefreshTokenCipher)
	if err != nil {
		return nil, fmt.Errorf("decrypt calendar grant for revocation")
	}
	revoker, ok := handler.provider.(TokenRevoker)
	if !ok {
		return nil, fmt.Errorf("calendar provider does not support revocation")
	}
	return func(revokeContext context.Context) error {
		if err := revoker.Revoke(revokeContext, refreshToken); err != nil {
			return fmt.Errorf("revoke calendar grant")
		}
		return nil
	}, nil
}
