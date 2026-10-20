package projection

import (
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

func TestOverrideExpiryBoundsProjectionLifetime(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{-time.Second, 0, 10 * time.Minute, 2 * time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			input := validInput(now, now.Add(2*BucketSize))
			open := input.Policy.Default
			input.Policy.Default = closedState()
			input.ManualOverrides = []policy.ManualOverride{{
				ID: "override", UserID: input.UserID, StartAt: now, EndAt: now.Add(BucketSize),
				CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(offset), State: open,
			}}
			got, err := NewEngine().Generate(input)
			if err != nil {
				t.Fatal(err)
			}
			want := closedState()
			expiry := now.Add(time.Hour)
			if offset > 0 {
				want = open
				if offset < time.Hour {
					expiry = now.Add(offset)
				}
			}
			if got[0].State != want || !got[0].ExpiresAt.Equal(expiry) {
				t.Fatalf("state/expiry mismatch: %+v", got[0])
			}
			if got[1].State != closedState() || !got[1].ExpiresAt.Equal(now.Add(time.Hour)) {
				t.Fatalf("non-overlapping bucket changed: %+v", got[1])
			}
		})
	}
}
