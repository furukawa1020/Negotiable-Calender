package firestorestore

import (
	"errors"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"strings"
	"testing"
	"time"
)

func TestStoredCalendarExportIntegrity(t *testing.T) {
	for _, scenario := range []string{"valid", "duplicate", "wrong-document", "wrong-option-request", "missing-org"} {
		t.Run(scenario, func(t *testing.T) {
			b, ctx := emulatorBackend(t)
			now := time.Now().UTC()
			v := confirmationRequest("r", "alice", "bob", now, now.Add(-2*time.Hour))
			v.Status, v.AcceptedOptionID, v.Title = coord.Accepted, v.Options[0].ID, "PRIVATE_TITLE"
			switch scenario {
			case "duplicate":
				v.Options = append(v.Options, v.Options[0])
			case "wrong-document":
				v.ID = "other"
				v.Options[0].RequestID = "other"
			case "wrong-option-request":
				v.Options[0].RequestID = "other"
			case "missing-org":
				v.OrganizationID = ""
			}
			ref := b.Client.Collection("coordinationRequests").Doc("r")
			putDocument(t, ctx, ref, v)
			before, err := ref.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, actor := range []string{"alice", "bob", "outsider"} {
				got, err := b.Request().GetForUser(ctx, "r", actor)
				if actor == "outsider" || scenario == "wrong-document" {
					if !errors.Is(err, coord.ErrNotFound) || got.ID != "" || got.Title != "" {
						t.Fatal("mismatched/private record returned", err)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				ics, err := coord.CalendarExport(got)
				if scenario == "valid" {
					if err != nil || !strings.Contains(ics, "BEGIN:VEVENT") {
						t.Fatal("valid past export failed", err)
					}
				} else if err == nil || ics != "" {
					t.Fatal("damaged meeting exported", err)
				}
			}
			after, err := ref.Get(ctx)
			if err != nil || !before.UpdateTime.Equal(after.UpdateTime) {
				t.Fatal("export mutated request", err)
			}
		})
	}
}
