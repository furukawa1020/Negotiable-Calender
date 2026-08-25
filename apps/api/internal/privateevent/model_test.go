package privateevent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPrivateEventFailsClosed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 5, 0, 0, 0, time.UTC)
	event := PrivateEvent{
		ID: "e1", UserID: "u1", ProviderEventID: "provider-1", CalendarID: "calendar-1",
		StartAt: now, EndAt: now.Add(time.Hour), BusyStatus: Busy, Visibility: VisibilityPrivate,
		TitleEncrypted: []byte("ciphertext"), CreatedAt: now, UpdatedAt: now,
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid private event rejected: %v", err)
	}
	_, err := json.Marshal(event)
	if !errors.Is(err, ErrSerializationForbidden) {
		t.Fatalf("private event serialization did not fail closed: %v", err)
	}
}
