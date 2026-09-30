package main

import (
	"encoding/json"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"strings"
	"testing"
	"time"
)

func TestProjectionEngineToLongMeetingCandidates(t *testing.T) {
	for _, minutes := range []int{30, 60} {
		now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
		input := coord.CandidateInput{Now: now, Request: coord.CoordinationRequest{
			ID: "request", OrganizationID: "org", RequesterUserID: "member", TargetUserID: "manager",
			Type: coord.Meeting, Title: "Synthetic long meeting", DurationMinutes: minutes, DeadlineAt: now.Add(4 * time.Hour),
			SyncPreference: coord.Either, Priority: coord.PriorityNormal, Status: coord.Pending, CreatedAt: now, UpdatedAt: now,
		}}
		from, to := input.Now.Add(time.Hour), input.Now.Add(4*time.Hour)
		input.Request.DeadlineAt = to
		state := policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
		values, err := projection.NewEngine().Generate(projection.GenerateInput{
			UserID: input.Request.TargetUserID, Timezone: "UTC", From: from, To: to, Now: input.Now,
			Policy: policy.SharingPolicy{ID: "policy", UserID: input.Request.TargetUserID, Default: state,
				WorkingHours: []policy.WorkingWindow{{Weekday: from.Weekday(), StartMinute: 9 * 60, EndMinute: 12 * 60}}, CreatedAt: input.Now, UpdatedAt: input.Now},
			Events: []privateevent.PrivateEvent{{ID: "private", UserID: input.Request.TargetUserID, ProviderEventID: "provider-secret", CalendarID: "calendar-secret",
				StartAt: from.Add(time.Hour), EndAt: from.Add(75 * time.Minute), BusyStatus: privateevent.Busy, Visibility: privateevent.VisibilityPrivate,
				TitleEncrypted: []byte("not-for-public"), CreatedAt: input.Now, UpdatedAt: input.Now}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 12 {
			t.Fatalf("expected raw 15-minute buckets, got %d", len(values))
		}
		input.Projections = values
		options, err := coord.GenerateCandidates(input)
		if err != nil || len(options) != 3 {
			t.Fatal(options, err)
		}
		input.Request.Options = options
		for _, option := range options {
			if option.Type != coord.OptionMeeting || option.EndAt.Sub(*option.StartAt) != time.Duration(minutes)*time.Minute {
				t.Fatalf("long candidate missing: %+v", option)
			}
			if _, err := coord.ConfirmableMeeting(input.Request, option.ID, input.Now); err != nil {
				t.Fatal(err)
			}
			if err := coord.ValidateMeetingAvailability(input.Request.TargetUserID, option, values, input.Now); err != nil {
				t.Fatal(err)
			}
		}
		encoded, _ := json.Marshal(options)
		for _, forbidden := range []string{"score", "provider-secret", "calendar-secret", "not-for-public"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatal("private candidate data leaked")
			}
		}
	}
}
