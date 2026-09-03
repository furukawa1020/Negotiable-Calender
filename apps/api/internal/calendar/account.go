package calendar

import (
	"context"
	"errors"
	"fmt"
)

func (handler *Handler) RevokeForAccountDeletion(ctx context.Context, userID string) error {
	connection, err := handler.store.GetConnection(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load calendar grant for revocation: %w", err)
	}
	if handler.cipher == nil {
		return fmt.Errorf("calendar token cipher is unavailable")
	}
	refreshToken, err := handler.cipher.Decrypt(connection.RefreshTokenCipher)
	if err != nil {
		return fmt.Errorf("decrypt calendar grant for revocation")
	}
	revoker, ok := handler.provider.(TokenRevoker)
	if !ok {
		return fmt.Errorf("calendar provider does not support revocation")
	}
	if err := revoker.Revoke(ctx, refreshToken); err != nil {
		return fmt.Errorf("revoke calendar grant")
	}
	return nil
}
