package projection

import (
	"testing"
	"time"
)

func TestGenerationBoundsBeforeAllocation(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 1, 0, time.UTC)
	for _, duration := range []time.Duration{120 * 24 * time.Hour, MaxGenerationRange, MaxGenerationRange + time.Nanosecond, time.Duration(1<<63 - 1)} {
		input := validInput(now, now.Add(duration))
		values, err := NewEngine().Generate(input)
		if duration > MaxGenerationRange {
			if err == nil || values != nil {
				t.Fatal("unbounded range accepted")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			want := int(duration/BucketSize) + 1
			if len(values) != want {
				t.Fatalf("buckets=%d want %d", len(values), want)
			}
		}
	}
}

func TestGenerationRejectsInvalidConfiguration(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	for _, ttl := range []time.Duration{-time.Second, 0, 24*time.Hour + time.Nanosecond} {
		if values, err := (Engine{ProjectionTTL: ttl}).Generate(validInput(now, now.Add(BucketSize))); err == nil || values != nil {
			t.Fatalf("invalid TTL accepted: %s", ttl)
		}
	}
	for _, zone := range []string{"", "Local", "not/a-zone"} {
		input := validInput(now, now.Add(BucketSize))
		input.Timezone = zone
		if values, err := NewEngine().Generate(input); err == nil || values != nil {
			t.Fatalf("invalid timezone accepted: %q", zone)
		}
	}
	input := validInput(now, now.Add(BucketSize))
	input.Timezone = "UTC"
	if _, err := (Engine{ProjectionTTL: 24 * time.Hour}).Generate(input); err != nil {
		t.Fatal(err)
	}
}
