package calendar

import (
	"sort"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

// CandidateAvailability is an immutable, private decision snapshot. It exposes
// only a slot predicate, not source receipts, event ranges or credentials.
// The zero value is unavailable, never proof of a never-connected account.
type CandidateAvailability struct {
	valid, managed              bool
	observed, expires, from, to time.Time
	busy                        []BusyInterval
}

func (CandidateAvailability) MarshalJSON() ([]byte, error) {
	return nil, privateevent.ErrSerializationForbidden
}

func NewCandidateAvailability(source SourceState, values []BusyInterval, now time.Time) (CandidateAvailability, error) {
	if len(values) > BusyEvidenceLimit || (source.Managed && !source.Committed(now)) {
		return CandidateAvailability{}, ErrSourceUnavailable
	}
	s := CandidateAvailability{valid: true, managed: source.Managed}
	if !source.Managed {
		return s, nil
	}
	s.observed, s.expires, s.from, s.to = source.Snapshot.ObservedAt, source.Snapshot.ObservedAt.Add(SourceMaxAge), source.Snapshot.From, source.Snapshot.To
	for _, v := range values {
		status := privateevent.BusyStatus(v.Status)
		if v.StartAt.IsZero() || !v.EndAt.After(v.StartAt) || !status.Valid() {
			return CandidateAvailability{}, ErrSourceUnavailable
		}
		if status != privateevent.Free {
			s.busy = append(s.busy, BusyInterval{StartAt: v.StartAt, EndAt: v.EndAt})
		}
	}
	sort.Slice(s.busy, func(i, j int) bool { return s.busy[i].StartAt.Before(s.busy[j].StartAt) })
	merged := s.busy[:0]
	for _, v := range s.busy {
		if len(merged) == 0 || merged[len(merged)-1].EndAt.Before(v.StartAt) {
			merged = append(merged, v)
		} else if v.EndAt.After(merged[len(merged)-1].EndAt) {
			merged[len(merged)-1].EndAt = v.EndAt
		}
	}
	s.busy = merged
	return s, nil
}

func (s CandidateAvailability) Validate(now time.Time) error {
	if !s.valid || now.IsZero() || (s.managed && (now.Before(s.observed) || !now.Before(s.expires))) {
		return ErrSourceUnavailable
	}
	return nil
}

func (s CandidateAvailability) Allows(from, to, now time.Time) bool {
	if s.Validate(now) != nil || from.IsZero() || !to.After(from) {
		return false
	}
	if !s.managed {
		return true
	}
	if from.Before(s.from) || to.After(s.to) {
		return false
	}
	i := sort.Search(len(s.busy), func(i int) bool { return s.busy[i].EndAt.After(from) })
	return i == len(s.busy) || !s.busy[i].StartAt.Before(to)
}
