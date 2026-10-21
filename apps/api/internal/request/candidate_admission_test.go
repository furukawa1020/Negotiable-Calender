package request

import (
	"testing"
	"time"
)

func TestCandidateAdmissionBeforeAsyncFallback(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, preference := range []SyncPreference{Either, AsyncPreferred} {
		for _, status := range []Status{Pending, Suggested, Accepted, Declined, Delegated, Cancelled, Expired, Completed, Async} {
			t.Run(string(preference)+"/"+string(status), func(t *testing.T) {
				v := validCandidateRequest(now)
				v.SyncPreference, v.Status = preference, status
				v.AsyncMessage = "follow up in writing"
				got, err := GenerateCandidates(CandidateInput{Request: v, Now: now})
				active := status == Pending || status == Suggested
				if active && (err != nil || len(got) != 1) {
					t.Fatalf("active request: %v %v", got, err)
				}
				if !active && (err == nil || got != nil) {
					t.Fatal("inactive request generated options")
				}
			})
		}
		for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
			v := validCandidateRequest(now.Add(-time.Hour))
			v.SyncPreference, v.DeadlineAt = preference, now.Add(offset)
			got, err := GenerateCandidates(CandidateInput{Request: v, Now: now})
			if offset <= 0 && (err == nil || got != nil) {
				t.Fatal("expired request generated options")
			}
			if offset > 0 && (err != nil || len(got) != 1) {
				t.Fatal("live deadline rejected", err)
			}
		}
		v := validCandidateRequest(now.Add(time.Nanosecond))
		v.SyncPreference = preference
		if got, err := GenerateCandidates(CandidateInput{Request: v, Now: now}); err == nil || got != nil {
			t.Fatal("future creation accepted")
		}
	}
}
