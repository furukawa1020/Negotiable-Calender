package httpapi

import (
	"context"
	"time"
)

const workspaceReadTimeout = 10 * time.Second

// A store must honor the context to release resources promptly. The second
// check also prevents publication if a store returns success after expiry.
func readWorkspace[T any](ctx context.Context, read func(context.Context) (T, error)) (T, error) {
	var empty T
	ctx, cancel := context.WithTimeout(ctx, workspaceReadTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	value, err := read(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return empty, contextErr
	}
	return value, err
}
