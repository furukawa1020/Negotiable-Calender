package request

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func BenchmarkCandidateReservationScan(b *testing.B) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ranges := make([]ReservedRange, 20000)
	for i := range ranges {
		start := now.Add(-time.Duration(i+1) * time.Hour)
		ranges[i] = ReservedRange{StartAt: start, EndAt: start.Add(time.Minute)}
	}
	for _, mode := range []string{"linear", "indexed"} {
		b.Run(mode, func(b *testing.B) {
			for range b.N {
				lookup := func(start, end time.Time) bool { return overlapsReserved(start, end, ranges) }
				if mode == "indexed" {
					index, err := buildReservationIndex(context.Background(), ranges)
					if err != nil {
						b.Fatal(err)
					}
					lookup = index.overlaps // Include normalization/build cost.
				}
				for i := range 11520 {
					start := now.Add(time.Duration(i) * 15 * time.Minute)
					if lookup(start, start.Add(30*time.Minute)) {
						b.Fatal("unexpected overlap")
					}
				}
			}
		})
	}
}

func TestReservationIndexMatchesLinearScan(t *testing.T) {
	random := rand.New(rand.NewSource(199))
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for range 200 {
		values := []ReservedRange{}
		for range random.Intn(80) {
			start := now.Add(time.Duration(random.Intn(200)-100) * time.Minute)
			values = append(values, ReservedRange{StartAt: start, EndAt: start.Add(time.Duration(1+random.Intn(20)) * time.Minute)})
		}
		before := append([]ReservedRange{}, values...)
		index, err := buildReservationIndex(context.Background(), values)
		if err != nil || !reflect.DeepEqual(before, values) {
			t.Fatal("source mutated", err)
		}
		for range 100 {
			start := now.Add(time.Duration(random.Intn(300)-150) * time.Minute)
			end := start.Add(time.Duration(1+random.Intn(60)) * time.Minute)
			if index.overlaps(start, end) != overlapsReserved(start, end, values) {
				t.Fatal("indexed/linear mismatch")
			}
		}
	}
}

func TestReservationIndexMergesWithoutClosingAdjacentFreeTime(t *testing.T) {
	now := time.Now().UTC()
	at := func(n int) time.Time { return now.Add(time.Duration(n) * time.Minute) }
	index, err := buildReservationIndex(context.Background(), []ReservedRange{{at(20), at(30)}, {at(0), at(10)}, {at(0), at(10)}, {at(2), at(5)}, {at(10), at(20)}})
	if err != nil || len(index) != 1 || !index[0].StartAt.Equal(at(0)) || !index[0].EndAt.Equal(at(30)) {
		t.Fatal("bad merge", index, err)
	}
	if index.overlaps(at(-10), at(0)) || index.overlaps(at(30), at(40)) || !index.overlaps(at(29), at(31)) {
		t.Fatal("half-open boundaries changed")
	}
}

func TestInvalidReservationIndexFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	for _, value := range []ReservedRange{{}, {now, now}, {now, now.Add(-time.Minute)}} {
		input := continuousCandidateInput(30)
		input.Reserved = []ReservedRange{value}
		if got, err := GenerateCandidates(input); !errors.Is(err, ErrAvailabilityChanged) || got != nil {
			t.Fatal("invalid evidence returned candidates", err)
		}
	}
}

// Independent linear oracle for equivalence tests and the before/after benchmark.
func overlapsReserved(startAt, endAt time.Time, reserved []ReservedRange) bool {
	for _, value := range reserved {
		if startAt.Before(value.EndAt) && value.StartAt.Before(endAt) {
			return true
		}
	}
	return false
}
