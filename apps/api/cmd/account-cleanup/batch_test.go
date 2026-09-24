package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/accountcleanup"
)

type testStore struct {
	calls  int
	cursor string
	fail   bool
}

func (s *testStore) ListPendingAccountDeletions(_ context.Context, limit int, cursor string) (accountcleanup.Page, error) {
	s.cursor = cursor
	return accountcleanup.Page{UserIDs: []string{"private-user"}, HasMore: true}, nil
}
func (s *testStore) ResumeAccountDeletion(context.Context, string) error {
	s.calls++
	if s.fail {
		return errors.New("private-user secret-provider-error")
	}
	return nil
}
func testFactory(store *testStore) factory {
	return func(context.Context, string) (accountcleanup.Store, func(), error) { return store, func() {}, nil }
}

func TestBatchFlagsFailBeforeBackendAccess(t *testing.T) {
	for _, args := range [][]string{
		{"-project", "demo-test", "-pending", "-user", "alice"},
		{"-project", "demo-test", "-pending", "-limit", "101"},
		{"-project", "demo-test", "-pending", "-limit", "0"},
		{"-project", "demo-test", "-pending", "-timeout", "16m"},
		{"-project", "demo-test", "-pending", "-account-timeout", "6m"},
		{"-project", "demo-test", "-user", "alice", "-limit", "1"},
		{"-project", "demo-test", "-user", "alice", "-dry-run"},
		{"-project", "demo-test", "-pending", "-limit", "sensitive-value"},
	} {
		var out bytes.Buffer
		err := execute(context.Background(), args, &out, func(context.Context, string) (accountcleanup.Store, func(), error) {
			t.Fatal("invalid flags opened backend")
			return nil, nil, nil
		})
		if err == nil || strings.Contains(out.String()+err.Error(), "sensitive-value") {
			t.Fatal("invalid flags accepted or reflected")
		}
	}
}

func TestCursorIsWrittenOnlyToExplicitNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint")
	s := &testStore{fail: true}
	var out bytes.Buffer
	err := execute(context.Background(), []string{"-project", "demo-test", "-pending", "-cursor-out", path}, &out, testFactory(s))
	if !errors.Is(err, accountcleanup.ErrIncomplete) || s.calls != 1 {
		t.Fatal("retry not performed")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != accountcleanup.EncodeCursor("private-user") {
		t.Fatal("cursor missing")
	}
	for _, secret := range []string{"private-user", "secret-provider-error", string(data), path} {
		if strings.Contains(out.String()+err.Error(), secret) {
			t.Fatal("cursor or provider data leaked to summary")
		}
	}
	var summary accountcleanup.Result
	if json.Unmarshal(out.Bytes(), &summary) != nil || summary.Failed != 1 {
		t.Fatal("counts missing")
	}
	before := s.calls
	err = execute(context.Background(), []string{"-project", "demo-test", "-pending", "-cursor-out", path}, &out, testFactory(s))
	if err == nil || s.calls != before {
		t.Fatal("existing output file overwritten or mutation occurred")
	}
	if after, _ := os.ReadFile(path); string(after) != string(data) {
		t.Fatal("existing checkpoint changed")
	}
}

func TestDryRunReadsCursorWithoutDeleting(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input")
	output := filepath.Join(dir, "output")
	if err := os.WriteFile(input, []byte(accountcleanup.EncodeCursor("a")), 0600); err != nil {
		t.Fatal(err)
	}
	s := &testStore{}
	var out bytes.Buffer
	err := execute(context.Background(), []string{"-project", "demo-test", "-pending", "-dry-run", "-cursor-in", input, "-cursor-out", output}, &out, testFactory(s))
	if err != nil || s.calls != 0 || s.cursor != accountcleanup.EncodeCursor("a") {
		t.Fatal("dry run mutated or ignored cursor")
	}
	if data, _ := os.ReadFile(output); string(data) != accountcleanup.EncodeCursor("private-user") {
		t.Fatal("dryrun pagination missing")
	}
}

func TestBackendErrorIsSanitizedAndCursorInputBounded(t *testing.T) {
	var out bytes.Buffer
	err := execute(context.Background(), []string{"-project", "demo-test", "-pending"}, &out, func(context.Context, string) (accountcleanup.Store, func(), error) {
		return nil, nil, errors.New("credential-secret")
	})
	if err == nil || strings.Contains(out.String()+err.Error(), "credential-secret") {
		t.Fatal("backend error leaked")
	}
	path := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 2049), 0600); err != nil {
		t.Fatal(err)
	}
	err = execute(context.Background(), []string{"-project", "demo-test", "-pending", "-cursor-in", path}, &out, func(context.Context, string) (accountcleanup.Store, func(), error) {
		t.Fatal("oversized cursor reached backend")
		return nil, nil, nil
	})
	if err == nil {
		t.Fatal("oversized cursor accepted")
	}
}
