package calendar

import (
	"context"
	"errors"
	"time"
)

const SourceMaxAge = 30 * time.Minute

var ErrSourceUnavailable = errors.New("source_unavailable")

// SourceSnapshot describes the provider data, never the time of a policy rebuild.
type SourceSnapshot struct {
	Revision             string
	ObservedAt, From, To time.Time
}

func (s SourceSnapshot) Valid() bool {
	return s.Revision != "" && !s.ObservedAt.IsZero() && !s.From.IsZero() && s.To.After(s.From)
}
func (s SourceSnapshot) Fresh(now time.Time) bool {
	return s.Valid() && !s.ObservedAt.After(now) && now.Before(s.ObservedAt.Add(SourceMaxAge))
}
func (s SourceSnapshot) Covers(from, to time.Time) bool {
	return s.Valid() && to.After(from) && !from.Before(s.From) && !to.After(s.To)
}

func NextSourceSnapshot(previous SourceSnapshot, full bool, revision string, from, to, now time.Time) (SourceSnapshot, error) {
	if !full {
		if !previous.Valid() {
			return SourceSnapshot{}, ErrSourceUnavailable
		}
		// A delta cursor proves nothing about a newly expanded time window.
		from, to = previous.From, previous.To
	}
	s := SourceSnapshot{Revision: revision, ObservedAt: now.UTC().Truncate(time.Microsecond), From: from.UTC(), To: to.UTC()}
	if !s.Valid() {
		return SourceSnapshot{}, ErrSourceUnavailable
	}
	return s, nil
}

type SourceState struct {
	Managed           bool
	Snapshot          SourceSnapshot
	PublishedRevision string
	Connection        *Connection
}

func (s SourceState) Committed(now time.Time) bool {
	c := s.Connection
	return s.Snapshot.Fresh(now) && c != nil && !c.ReconnectRequired && c.LastErrorCode == "" && c.SyncLeaseID == "" && c.LastSyncedAt != nil && c.LastSyncedAt.UTC().Truncate(time.Microsecond).Equal(s.Snapshot.ObservedAt)
}
func (s SourceState) Readable(now time.Time) bool {
	return !s.Managed || (s.Committed(now) && s.PublishedRevision == s.Snapshot.Revision)
}
func (s SourceState) Rebuildable(ctx context.Context, userID string, now time.Time) bool {
	if !s.Managed || s.Committed(now) {
		return true
	}
	lease, ok := SyncLeaseFromContext(ctx)
	c := s.Connection
	return s.Snapshot.Fresh(now) && c != nil && !c.ReconnectRequired && ok && lease.UserID == userID && lease.ID != "" && lease.ID == c.SyncLeaseID && c.SyncLeaseUntil != nil && c.SyncLeaseUntil.After(now)
}

type sourceContextKey struct{}
type capturedSource struct {
	UserID string
	State  SourceState
}

func WithSourceSnapshot(ctx context.Context, userID string, state SourceState) context.Context {
	return context.WithValue(ctx, sourceContextKey{}, capturedSource{userID, state})
}
func CapturedSource(ctx context.Context, userID string) (SourceState, bool) {
	v, ok := ctx.Value(sourceContextKey{}).(capturedSource)
	return v.State, ok && v.UserID == userID
}

type SourceSnapshotStore interface {
	LoadSourceSnapshot(context.Context, string) (SourceSnapshot, error)
}
