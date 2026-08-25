package projection

import (
	"testing"
	"time"
)

func TestNormalizeTimestampsUsesExplicitUTC(t *testing.T) {
	t.Parallel()
	driverLocation := time.FixedZone("driver-utc", 0)
	value := ScheduleProjection{
		StartAt:     time.Date(2026, 8, 26, 0, 0, 0, 0, driverLocation),
		EndAt:       time.Date(2026, 8, 26, 1, 0, 0, 0, driverLocation),
		GeneratedAt: time.Date(2026, 8, 25, 23, 0, 0, 0, driverLocation),
		ExpiresAt:   time.Date(2026, 8, 27, 0, 0, 0, 0, driverLocation),
	}
	result := normalizeTimestamps(value)
	for name, timestamp := range map[string]time.Time{
		"start": result.StartAt, "end": result.EndAt,
		"generated": result.GeneratedAt, "expires": result.ExpiresAt,
	} {
		if timestamp.Location() != time.UTC {
			t.Fatalf("%s timestamp was not normalized to UTC", name)
		}
	}
}
