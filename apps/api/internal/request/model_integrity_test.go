package request

import (
	"math"
	"testing"
	"time"
)

func TestRequestRejectsAmbiguousOptionOwnership(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"valid", "foreign", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			value := validCandidateRequest(now)
			value.Options = []Option{{ID: "o", RequestID: value.ID, Type: OptionAsync, CreatedAt: now}}
			if scenario == "foreign" {
				value.Options[0].RequestID = "other"
			}
			if scenario == "duplicate" {
				value.Options = append(value.Options, value.Options[0])
			}
			if err := value.Validate(); (err == nil) != (scenario == "valid") {
				t.Fatalf("validation=%v", err)
			}
		})
	}
}

func TestDurationRangeAppliesBeforeAsyncFallback(t *testing.T) {
	now := time.Now().UTC()
	for _, minutes := range []int{0, -1, int(math.MaxInt64/int64(time.Minute)) + 1, 1, int(math.MaxInt64 / int64(time.Minute))} {
		for _, pref := range []SyncPreference{SyncPreferred, AsyncPreferred} {
			value := validCandidateRequest(now)
			value.DurationMinutes = minutes
			value.SyncPreference = pref
			valid := minutes > 0 && int64(minutes) <= math.MaxInt64/int64(time.Minute)
			if err := value.Validate(); (err == nil) != valid {
				t.Fatalf("minutes=%d err=%v", minutes, err)
			}
			if !valid {
				if _, err := GenerateCandidates(CandidateInput{Request: value, Now: now}); err == nil {
					t.Fatal("invalid duration generated candidates")
				}
			}
		}
	}
}

func TestOptionalCandidateTimesMustBeValidUTC(t *testing.T) {
	now := time.Now().UTC()
	for _, field := range []string{"start", "end", "response"} {
		for _, stamp := range []time.Time{time.Time{}, now.In(time.FixedZone("offset", 3600)), now} {
			option := Option{ID: "o", RequestID: "r", Type: OptionAsync, CreatedAt: now}
			switch field {
			case "start":
				option.StartAt = &stamp
			case "end":
				option.EndAt = &stamp
			case "response":
				option.ResponseBy = &stamp
			}
			valid := !stamp.IsZero() && stamp.Location() == time.UTC
			if err := option.Validate(); (err == nil) != valid {
				t.Fatalf("field=%s stamp=%v err=%v", field, stamp, err)
			}
		}
	}
}
