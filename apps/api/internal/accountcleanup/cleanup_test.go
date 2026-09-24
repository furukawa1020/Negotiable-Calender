package accountcleanup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	page    Page
	listErr error
	calls   []string
	resume  func(context.Context, string) error
	list    func(context.Context, int, string) (Page, error)
}

func (f *fakeStore) ListPendingAccountDeletions(ctx context.Context, limit int, cursor string) (Page, error) {
	if f.list != nil {
		return f.list(ctx, limit, cursor)
	}
	return f.page, f.listErr
}
func (f *fakeStore) ResumeAccountDeletion(ctx context.Context, id string) error {
	f.calls = append(f.calls, id)
	if f.resume != nil {
		return f.resume(ctx, id)
	}
	return nil
}
func options() Options {
	return Options{Pending: true, Limit: 25, Timeout: time.Minute, PerAccountTimeout: time.Second}
}

func TestFailureContinuesAndSummaryContainsNoIDsOrProviderErrors(t *testing.T) {
	f := &fakeStore{page: Page{UserIDs: []string{"private-alice", "private-bob"}, HasMore: true}}
	f.resume = func(_ context.Context, id string) error {
		if id == "private-alice" {
			return errors.New("secret provider payload")
		}
		return nil
	}
	r, err := Run(context.Background(), f, options())
	if !errors.Is(err, ErrIncomplete) || r.Attempted != 2 || r.Failed != 1 || r.Succeeded != 1 || !r.HasMore || r.NextCursor != EncodeCursor("private-bob") {
		t.Fatalf("unexpected counts: %+v %v", r, err)
	}
	data, _ := json.Marshal(r)
	for _, secret := range []string{"private-alice", "private-bob", r.NextCursor, "secret provider payload"} {
		if strings.Contains(string(data)+err.Error(), secret) {
			t.Fatal("sensitive data leaked")
		}
	}
}

func TestPerAccountDeadlineDoesNotStopLaterAccounts(t *testing.T) {
	f := &fakeStore{page: Page{UserIDs: []string{"a", "b"}}}
	f.resume = func(ctx context.Context, id string) error {
		if id == "a" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	o := options()
	o.PerAccountTimeout = 10 * time.Millisecond
	r, err := Run(context.Background(), f, o)
	if !errors.Is(err, ErrIncomplete) || r.Attempted != 2 || r.Failed != 1 || r.Succeeded != 1 || r.Interrupted {
		t.Fatalf("unexpected timeout counts: %+v %v", r, err)
	}
}

func TestOverallCancellationStopsBeforeNextAccountAndReturnsProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeStore{page: Page{UserIDs: []string{"a", "b", "c"}}}
	f.resume = func(context.Context, string) error { cancel(); return nil }
	r, err := Run(ctx, f, options())
	if !errors.Is(err, ErrIncomplete) || len(f.calls) != 1 || r.Remaining != 2 || !r.Interrupted || !r.HasMore || r.NextCursor != EncodeCursor("a") {
		t.Fatalf("bad checkpoint: %+v %v", r, err)
	}
}

func TestGlobalDeadlineIncludesListing(t *testing.T) {
	f := &fakeStore{list: func(ctx context.Context, _ int, _ string) (Page, error) { <-ctx.Done(); return Page{}, ctx.Err() }}
	o := options()
	o.Timeout = 10 * time.Millisecond
	r, err := Run(context.Background(), f, o)
	if !errors.Is(err, ErrIncomplete) || !r.Interrupted || !r.ListingFailed || len(f.calls) != 0 {
		t.Fatal("listing was not bounded")
	}
}

func TestDryRunNeverResumesAndEndOfSweepClearsCursor(t *testing.T) {
	f := &fakeStore{page: Page{UserIDs: []string{"b"}}}
	o := options()
	o.Cursor = EncodeCursor("a")
	o.DryRun = true
	r, err := Run(context.Background(), f, o)
	if err != nil || len(f.calls) != 0 || r.Selected != 1 || !r.DryRun || r.NextCursor != "" || r.HasMore {
		t.Fatalf("dry run mutated: %+v %v", r, err)
	}
	f.page = Page{}
	r, err = Run(context.Background(), f, options())
	if err != nil || r.Selected != 0 || r.HasMore || len(f.calls) != 0 {
		t.Fatal("empty sweep failed")
	}
}

func TestInvalidPagesNeverMutate(t *testing.T) {
	for _, page := range []Page{{UserIDs: []string{"a", "a"}}, {UserIDs: []string{"b", "a"}}, {UserIDs: []string{"path/child"}}, {HasMore: true}} {
		f := &fakeStore{page: page}
		_, err := Run(context.Background(), f, options())
		if err == nil || len(f.calls) > 0 {
			t.Fatal("invalid page accepted")
		}
	}
	f := &fakeStore{page: Page{UserIDs: []string{"a", "b"}}}
	o := options()
	o.Limit = 1
	if _, err := Run(context.Background(), f, o); err == nil || len(f.calls) > 0 {
		t.Fatal("oversized page accepted")
	}
}

func TestCursorValidation(t *testing.T) {
	for _, value := range []string{"!", EncodeCursor("a/b"), EncodeCursor(".."), EncodeCursor(""), EncodeCursor("a\nb"), strings.Repeat("a", 2049)} {
		if _, err := DecodeCursor(value); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	for _, id := range []string{"alice", "日本語", strings.Repeat("a", 1500)} {
		actual, err := DecodeCursor(EncodeCursor(id))
		if err != nil || actual != id {
			t.Fatal("cursor roundtrip failed")
		}
	}
}

func TestInvalidOptionsFailBeforeAccess(t *testing.T) {
	for _, mutate := range []func(*Options){func(o *Options) { o.Limit = 0 }, func(o *Options) { o.Limit = 101 }, func(o *Options) { o.User = "a" }, func(o *Options) { o.Pending = false }, func(o *Options) { o.Timeout = 16 * time.Minute }, func(o *Options) { o.PerAccountTimeout = 0 }, func(o *Options) { o.PerAccountTimeout = 6 * time.Minute }, func(o *Options) { o.Cursor = "secret-invalid" }} {
		o := options()
		mutate(&o)
		if _, err := Run(context.Background(), nil, o); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestSingleTargetDoesNotEnumerate(t *testing.T) {
	f := &fakeStore{list: func(context.Context, int, string) (Page, error) {
		t.Fatal("single mode enumerated accounts")
		return Page{}, nil
	}}
	o := options()
	o.Pending = false
	o.User = "alice"
	r, err := Run(context.Background(), f, o)
	if err != nil || r.Succeeded != 1 || len(f.calls) != 1 || f.calls[0] != "alice" {
		t.Fatal("single retry failed")
	}
}
