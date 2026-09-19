package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type creationTestStore struct {
	stubRequestStore
	committed bool
	creates   int
	lookupErr error
}

func (s *creationTestStore) LookupCreation(_ context.Context, value coordinationrequest.CoordinationRequest) (coordinationrequest.CoordinationRequest, error) {
	if s.lookupErr != nil {
		return coordinationrequest.CoordinationRequest{}, s.lookupErr
	}
	if !s.committed {
		return coordinationrequest.CoordinationRequest{}, coordinationrequest.ErrNotFound
	}
	if !coordinationrequest.SameCreation(s.value, value) {
		return coordinationrequest.CoordinationRequest{}, coordinationrequest.ErrCreationConflict
	}
	return s.value, nil
}
func (s *creationTestStore) CreateOnce(_ context.Context, value coordinationrequest.CoordinationRequest) (bool, error) {
	s.value = value
	s.committed = true
	s.creates++
	return true, nil
}

func TestCreationCommandReplayAndConflict(t *testing.T) {
	store := &creationTestStore{}
	notes, audits := &stubNotificationStore{}, &stubAuditStore{}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, notes, audits, "", testLogger())
	input := createCoordinationRequestInput{TargetUserID: "bob", Type: coordinationrequest.Review, Title: "review", DurationMinutes: 15, DeadlineAt: time.Now().UTC().Add(time.Hour), SyncPreference: coordinationrequest.Either, Priority: coordinationrequest.PriorityNormal}
	send := func(key string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
		req.Header.Set("X-Demo-User-ID", "alice")
		req.Header.Set("X-Organization-ID", "org")
		req.Header.Set("Idempotency-Key", key)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	const key = "command-123456789012345"
	first := send(key)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	id := store.value.ID
	store.value.Status = coordinationrequest.Cancelled
	// A replay must bypass fresh candidate generation and preserve current state.
	replay := send(key)
	if replay.Code != 200 || replay.Header().Get("Idempotency-Replayed") != "true" || store.value.ID != id || store.creates != 1 {
		t.Fatal("not idempotent", replay.Code, replay.Body.String())
	}
	if len(notes.values) != 0 || len(audits.values) != 0 {
		t.Fatal("HTTP duplicated atomic effects")
	}
	// Existing commands remain recoverable after their deadline passes.
	input.DeadlineAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	store.value.DeadlineAt = input.DeadlineAt
	if result := send(key); result.Code != 200 || store.creates != 1 {
		t.Fatal("expired command replay failed", result.Code)
	}
	input.Title = "changed"
	if result := send(key); result.Code != 409 {
		t.Fatal("changed body accepted", result.Code)
	}
	if result := send("short"); result.Code != 400 {
		t.Fatal("invalid key accepted", result.Code)
	}
	input.Title = "review"
	store.lookupErr = errors.New("storage unavailable")
	if result := send(key); result.Code != 503 || store.creates != 1 {
		t.Fatal("lookup failure created duplicate")
	}
	store.lookupErr = coordinationrequest.ErrCreationForbidden
	if result := send(key); result.Code != 403 {
		t.Fatal("membership failure accepted", result.Code)
	}
}
