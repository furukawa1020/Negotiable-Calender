package request

import (
	"testing"
	"time"
)

func TestAvailabilityRejectsMalformedIntervalBeforeCoverage(t *testing.T) {
	now := time.Now().UTC()
	end := now.Add(time.Hour)
	zero := time.Time{}
	local := now.In(time.FixedZone("JST", 9*3600))
	for _, option := range []Option{{}, {StartAt: &now, EndAt: &now}, {StartAt: &end, EndAt: &now}, {StartAt: &zero, EndAt: &end}, {StartAt: &local, EndAt: &end}} {
		if err := ValidateMeetingAvailability("u", option, nil, now); err != ErrCandidateInvalid {
			t.Fatalf("malformed range accepted: %v", err)
		}
	}
	valid := Option{StartAt: &now, EndAt: &end}
	if ValidateMeetingAvailability("", valid, nil, now) != ErrCandidateInvalid {
		t.Fatal("empty owner accepted")
	}
	if ValidateMeetingAvailability("u", valid, nil, time.Time{}) != ErrCandidateInvalid {
		t.Fatal("zero clock accepted")
	}
	if ValidateMeetingAvailability("u", valid, nil, now) != ErrAvailabilityChanged {
		t.Fatal("valid range bypassed coverage check")
	}
}
