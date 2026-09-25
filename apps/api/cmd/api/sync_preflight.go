package main

import (
	"context"
	"errors"
)

type syncQueryChecker interface {
	CheckSyncQueries(context.Context) error
}

func preflightFirestoreSync(ctx context.Context, store syncQueryChecker, mode string) error {
	if mode == "off" {
		return nil
	}
	if err := store.CheckSyncQueries(ctx); err != nil {
		// main logs startup errors. Never wrap the provider error: it may contain
		// an index creation URL, project metadata or a document identifier.
		return errors.New("calendar sync query preflight failed; check required indexes and runtime access")
	}
	return nil
}
