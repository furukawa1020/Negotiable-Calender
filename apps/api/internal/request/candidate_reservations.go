package request

import (
	"context"
	"sort"
	"time"
)

type reservationIndex []ReservedRange

// Copy before sorting: callers may share immutable source snapshots. Merging
// nested, overlapping and touching ranges makes ends monotonic for binary search.
func buildReservationIndex(ctx context.Context, values []ReservedRange) (reservationIndex, error) {
	ranges := make(reservationIndex, len(values))
	for i, value := range values {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if value.StartAt.IsZero() || !value.EndAt.After(value.StartAt) {
			return nil, ErrAvailabilityChanged
		}
		ranges[i] = value
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].StartAt.Before(ranges[j].StartAt) })
	merged := ranges[:0]
	for _, value := range ranges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(merged) == 0 || merged[len(merged)-1].EndAt.Before(value.StartAt) {
			merged = append(merged, value)
		} else if value.EndAt.After(merged[len(merged)-1].EndAt) {
			merged[len(merged)-1].EndAt = value.EndAt
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return merged, nil
}

func (ranges reservationIndex) overlaps(start, end time.Time) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].EndAt.After(start) })
	return i < len(ranges) && ranges[i].StartAt.Before(end)
}
