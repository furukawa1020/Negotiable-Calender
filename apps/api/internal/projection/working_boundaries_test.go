package projection

import (
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
)

func TestWorkingHoursUseHalfOpenElapsedBuckets(t *testing.T) {
	for _, tc := range []struct {
		name, zone, start string
		weekday           time.Weekday
		from, to          int
		want              bool
	}{
		{"midnight end", "Asia/Tokyo", "2026-08-24T14:45:00Z", time.Monday, 23 * 60, 24 * 60, true},
		{"after midnight", "Asia/Tokyo", "2026-08-24T15:00:00Z", time.Monday, 23 * 60, 24 * 60, false},
		{"partial end", "Asia/Tokyo", "2026-08-24T14:45:00Z", time.Monday, 23 * 60, 23*60 + 50, false},
		{"fold last bucket", "America/New_York", "2026-11-01T05:45:00Z", time.Sunday, 60, 120, true},
		{"fold outside window", "America/New_York", "2026-11-01T05:45:00Z", time.Sunday, 60, 90, false},
		{"fold repeated hour", "America/New_York", "2026-11-01T06:00:00Z", time.Sunday, 60, 120, true},
		{"spring gap boundary", "America/New_York", "2026-03-08T06:45:00Z", time.Sunday, 60, 120, true},
		{"spring after gap", "America/New_York", "2026-03-08T07:00:00Z", time.Sunday, 60, 120, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, err := time.Parse(time.RFC3339, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			input := validInput(start, start.Add(BucketSize))
			input.Timezone = tc.zone
			input.Policy.WorkingHours = []policy.WorkingWindow{{Weekday: tc.weekday, StartMinute: tc.from, EndMinute: tc.to}}
			values, err := NewEngine().Generate(input)
			if err != nil {
				t.Fatal(err)
			}
			if got := values[0].State.Availability == policy.Available; got != tc.want {
				t.Fatalf("available=%v want %v", got, tc.want)
			}
		})
	}
}
