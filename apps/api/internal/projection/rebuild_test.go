package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

type rebuildStoreStub struct {
	timezone string
	events   []privateevent.PrivateEvent
	replaced []ScheduleProjection
	err      error
}

func (store *rebuildStoreStub) UserTimezone(context.Context, string) (string, error) {
	return store.timezone, store.err
}
func (store *rebuildStoreStub) ListPrivateEvents(context.Context, string, time.Time, time.Time) ([]privateevent.PrivateEvent, error) {
	return store.events, store.err
}
func (store *rebuildStoreStub) Replace(_ context.Context, _ string, _ time.Time, _ time.Time, values []ScheduleProjection) error {
	store.replaced = values
	return store.err
}

type rebuildPolicyStub struct {
	value     policy.SharingPolicy
	overrides []policy.ManualOverride
	getErr    error
	upserted  bool
}

func (store *rebuildPolicyStub) Get(context.Context, string) (policy.SharingPolicy, error) {
	return store.value, store.getErr
}
func (store *rebuildPolicyStub) Upsert(_ context.Context, value policy.SharingPolicy) error {
	store.value = value
	store.upserted = true
	store.getErr = nil
	return nil
}
func (store *rebuildPolicyStub) ListActiveOverrides(context.Context, string, time.Time) ([]policy.ManualOverride, error) {
	return store.overrides, nil
}
func (*rebuildPolicyStub) CreateOverride(context.Context, policy.ManualOverride) error { return nil }

func TestRebuilderAppliesBusyEventsAndManualOverrideLast(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	from, to := now.Add(9*time.Hour), now.Add(10*time.Hour)
	store := &rebuildStoreStub{timezone: "UTC", events: []privateevent.PrivateEvent{{
		ID: "event-1", UserID: "user-1", ProviderEventID: "google-1", CalendarID: "primary",
		StartAt: from, EndAt: to, BusyStatus: privateevent.Busy, Visibility: privateevent.VisibilityDefault,
		CreatedAt: now, UpdatedAt: now,
	}}}
	policies := &rebuildPolicyStub{value: defaultPolicy("user-1", now)}
	policies.overrides = []policy.ManualOverride{{
		ID: "override-1", UserID: "user-1", StartAt: from.Add(15 * time.Minute), EndAt: from.Add(30 * time.Minute),
		State:     policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh},
		ExpiresAt: to, CreatedAt: now,
	}}
	if err := NewRebuilder(store, policies).Rebuild(context.Background(), "user-1", from, to, now); err != nil {
		t.Fatal(err)
	}
	if len(store.replaced) != 4 {
		t.Fatalf("expected four buckets, got %d", len(store.replaced))
	}
	if store.replaced[0].State.Availability != policy.Unavailable {
		t.Fatal("busy event did not close first bucket")
	}
	if store.replaced[1].State.Availability != policy.Available {
		t.Fatal("manual override did not win over busy event")
	}
}

func TestRebuilderCreatesDefaultPolicyForFirstSync(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	store := &rebuildStoreStub{timezone: "Asia/Tokyo"}
	policies := &rebuildPolicyStub{getErr: policy.ErrNotFound}
	if err := NewRebuilder(store, policies).Rebuild(context.Background(), "user-1", now, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if !policies.upserted || policies.value.UserID != "user-1" {
		t.Fatal("default policy was not persisted")
	}
}

func TestRebuilderDoesNotPublishWhenInputsFail(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	store := &rebuildStoreStub{timezone: "UTC", err: errors.New("database unavailable")}
	err := NewRebuilder(store, &rebuildPolicyStub{}).Rebuild(context.Background(), "user-1", now, now.Add(time.Hour), now)
	if err == nil {
		t.Fatal("input failure was ignored")
	}
	if store.replaced != nil {
		t.Fatal("partial projections were published")
	}
}

func TestRebuilderDoesNotPublishWhenPrivateEventIsInvalid(t *testing.T) {
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	store := &rebuildStoreStub{timezone: "UTC", events: []privateevent.PrivateEvent{{
		ID: "event-1", UserID: "user-1", ProviderEventID: "google-1", CalendarID: "primary",
		StartAt: now.Add(time.Hour), EndAt: now, BusyStatus: privateevent.Busy,
		Visibility: privateevent.VisibilityDefault, CreatedAt: now, UpdatedAt: now,
	}}}
	err := NewRebuilder(store, &rebuildPolicyStub{value: defaultPolicy("user-1", now)}).
		Rebuild(context.Background(), "user-1", now, now.Add(2*time.Hour), now)
	if err == nil {
		t.Fatal("invalid private event was accepted")
	}
	if store.replaced != nil {
		t.Fatal("invalid input published partial projections")
	}
}
