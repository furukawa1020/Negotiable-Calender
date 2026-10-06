package calendar

import (
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

// BusyEvidenceLimit bounds the entire synced cache, including malformed rows.
// Overflow must never turn a truncated cache into evidence of free time.
const BusyEvidenceLimit = 5000

// BusyInterval is private storage evidence, not an API response or projection.
// Titles, provider identifiers and other event details are deliberately absent.
type BusyInterval struct {
	StartAt, EndAt time.Time
	Status         string
}

func (BusyInterval) MarshalJSON() ([]byte, error) { return nil, privateevent.ErrSerializationForbidden }

// PrivateReadable does not require public policy publication: a requester's
// calendar conflicts must not depend on whether they accept incoming requests.
func (s SourceState) PrivateReadable(from, to, now time.Time) bool {
	return !from.IsZero() && to.After(from) && (!s.Managed || (s.Committed(now) && s.Snapshot.Covers(from, to)))
}

func ValidateBusyIntervals(source SourceState, intervals []BusyInterval, from, to, now time.Time) error {
	if !source.PrivateReadable(from, to, now) || len(intervals) > BusyEvidenceLimit {
		return ErrSourceUnavailable
	}
	if !source.Managed {
		return nil
	} // Never-connected policy-only accounts.
	for _, interval := range intervals {
		status := privateevent.BusyStatus(interval.Status)
		if interval.StartAt.IsZero() || !interval.EndAt.After(interval.StartAt) || !status.Valid() {
			return ErrSourceUnavailable
		}
		if status != privateevent.Free && interval.StartAt.Before(to) && from.Before(interval.EndAt) {
			return ErrSourceUnavailable
		}
	}
	return nil
}
