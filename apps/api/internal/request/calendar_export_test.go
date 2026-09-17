package request

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func confirmedFixture() CoordinationRequest {
	start := time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	return CoordinationRequest{ID: "request", OrganizationID: "org", Title: "相談", Status: Accepted, AcceptedOptionID: "chosen", UpdatedAt: start, Options: []Option{{ID: "chosen", RequestID: "request", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: start}}}
}

func TestCalendarExportStablePrivateAndEscaped(t *testing.T) {
	value := confirmedFixture()
	value.Title = strings.Repeat("相談", 50) + ",semi;slash\\\r\nATTENDEE:injected@example.test"
	contents, err := CalendarExport(value)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := CalendarExport(value)
	if contents != again {
		t.Fatal("export not stable")
	}
	for _, line := range strings.Split(contents, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Fatalf("invalid fold: %q", line)
		}
		if strings.HasPrefix(line, "ATTENDEE:") || strings.HasPrefix(line, "LOCATION:") || strings.HasPrefix(line, "ORGANIZER:") {
			t.Fatal("private property leaked/injected")
		}
	}
	unfolded := strings.ReplaceAll(contents, "\r\n ", "")
	for _, want := range []string{"DTSTART:20260921T010000Z", "DTEND:20260921T020000Z", "CLASS:PRIVATE", `\,semi\;slash\\\nATTENDEE:`} {
		if !strings.Contains(unfolded, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestCalendarExportRejectsUnconfirmedOrInvalidMeeting(t *testing.T) {
	for _, mutate := range []func(*CoordinationRequest){
		func(v *CoordinationRequest) { v.Status = Suggested }, func(v *CoordinationRequest) { v.Status = Cancelled },
		func(v *CoordinationRequest) { v.AcceptedOptionID = "other" }, func(v *CoordinationRequest) { v.Options[0].Type = OptionAsync },
		func(v *CoordinationRequest) { v.Options[0].EndAt = v.Options[0].StartAt }, func(v *CoordinationRequest) { v.Options[0].StartAt = nil },
	} {
		value := confirmedFixture()
		mutate(&value)
		if _, err := CalendarExport(value); err == nil {
			t.Fatal("invalid export accepted")
		}
	}
}
