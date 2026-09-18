package httpapi

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	request "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

// Optional capability: an unsupported backend must fail closed, not call an
// unbounded historical list. The production Firestore store implements this.
type planningSourceStore interface {
	LoadPlanningSources(context.Context, string, string, time.Time, time.Time) ([]projection.ScheduleProjection, []request.CoordinationRequest, error)
}

const planningRequestsPerMinute = 12
const planningMaxAccounts = 4096

type planningWindow struct {
	count int
	until time.Time
}
type planningBudget struct {
	mu       sync.Mutex
	accounts map[[32]byte]planningWindow
}

// Per authenticated account, not session/workspace/IP. Process-local protection
// (resets on restart); this is not a distributed quota or a billing guarantee.
func (b *planningBudget) allow(user string, now time.Time) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.accounts == nil {
		b.accounts = make(map[[32]byte]planningWindow)
	}
	key := sha256.Sum256([]byte(user))
	window, exists := b.accounts[key]
	if !exists || !now.Before(window.until) {
		if !exists && len(b.accounts) >= planningMaxAccounts {
			for key, value := range b.accounts {
				if !now.Before(value.until) {
					delete(b.accounts, key)
				}
			}
			if len(b.accounts) >= planningMaxAccounts {
				return false, time.Minute
			}
		}
		b.accounts[key] = planningWindow{count: 1, until: now.Add(time.Minute)}
		return true, 0
	}
	if window.count >= planningRequestsPerMinute {
		return false, window.until.Sub(now)
	}
	window.count++
	b.accounts[key] = window
	return true, 0
}
