package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
)

func TestNotificationResultOwnership(t *testing.T) {
	for _, scenario := range []string{"self", "foreign", "missing-owner", "mixed", "empty", "anonymous"} {
		t.Run(scenario, func(t *testing.T) {
			values := []notification.Notification{{ID: "n", UserID: "alice", Message: "PRIVATE"}}
			actor, want := "alice", 200
			switch scenario {
			case "foreign":
				values[0].UserID = "bob"
				want = 500
			case "missing-owner":
				values[0].UserID = ""
				want = 500
			case "mixed":
				values = append(values, notification.Notification{UserID: "bob", Message: "PRIVATE"})
				want = 500
			case "empty":
				values = nil
			case "anonymous":
				actor = ""
				want = 401
			}
			store := &stubNotificationStore{values: values}
			h := NewWithNotifications(nil, nil, nil, nil, nil, store, "", testLogger())
			r := httptest.NewRequest("GET", "/api/v1/notifications", nil)
			r.Header.Set("X-Demo-User-ID", actor)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if want != 200 && strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("notification leaked")
			}
			if scenario == "empty" && strings.TrimSpace(w.Body.String()) != `{"notifications":[]}` {
				t.Fatal(w.Body)
			}
			if scenario == "anonymous" && store.listUser != "" {
				t.Fatal("unauthorized read")
			}
		})
	}
}
