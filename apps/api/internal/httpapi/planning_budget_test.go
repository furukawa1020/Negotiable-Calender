package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlanningBudgetConcurrencyExpiryAndCapacity(t *testing.T) {
	var b planningBudget
	now := time.Now()
	var admitted atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 100; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if allowed, _ := b.allow("owner", now); allowed {
				admitted.Add(1)
			}
		}()
	}
	group.Wait()
	if admitted.Load() != planningRequestsPerMinute {
		t.Fatalf("admitted %d", admitted.Load())
	}
	if allowed, retry := b.allow("owner", now.Add(30*time.Second)); allowed || retry != 30*time.Second {
		t.Fatal("quota/reset boundary")
	}
	if allowed, _ := b.allow("owner", now.Add(time.Minute)); !allowed {
		t.Fatal("window did not reset")
	}
	b = planningBudget{}
	for i := 0; i < planningMaxAccounts; i++ {
		if allowed, _ := b.allow(fmt.Sprint(i), now); !allowed {
			t.Fatal("early capacity rejection")
		}
	}
	if allowed, _ := b.allow("overflow", now); allowed {
		t.Fatal("unbounded limiter memory")
	}
	if allowed, _ := b.allow("overflow", now.Add(time.Minute)); !allowed {
		t.Fatal("expired entries not reclaimed")
	}
}

func TestPlanningQuotaBeforeSourceReadsAndAcrossWorkspaces(t *testing.T) {
	requests, projections, organizations := planningFixture()
	store := &boundedPlanningStub{stubRequestStore: requests, projections: projections}
	handler := New(&stubDatabase{}, &stubPolicyStore{}, projections, organizations, store, "http://localhost:5173", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < planningRequestsPerMinute+2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/requests/private-request/planning-preview", nil)
		r.Header.Set("X-Demo-User-ID", "owner")
		r.Header.Set("X-Organization-ID", "workspace")
		r.AddCookie(&http.Cookie{Name: "negotiable_session", Value: fmt.Sprint(i)})
		if i >= planningRequestsPerMinute {
			r.Header.Set("X-Organization-ID", "different-workspace")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i < planningRequestsPerMinute && w.Code != 200 {
			t.Fatalf("%d: %d %s", i, w.Code, w.Body)
		}
		if i >= planningRequestsPerMinute && (w.Code != 429 || w.Header().Get("Retry-After") == "" || strings.Contains(w.Body.String(), "owner")) {
			t.Fatalf("quota response %d %s", w.Code, w.Body)
		}
	}
	if store.reads != planningRequestsPerMinute || store.loads != planningRequestsPerMinute {
		t.Fatalf("throttled request reached storage: reads=%d loads=%d", store.reads, store.loads)
	}
}

func TestPlanningUnsupportedBackendDoesNotFallBack(t *testing.T) {
	requests, projections, organizations := planningFixture()
	handler := New(&stubDatabase{}, &stubPolicyStore{}, projections, organizations, requests, "http://localhost:5173", slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requests/private-request/planning-preview", nil)
	r.Header.Set("X-Demo-User-ID", "owner")
	r.Header.Set("X-Organization-ID", "workspace")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || requests.getUser != "" || requests.target != "" {
		t.Fatalf("unbounded fallback: %d", w.Code)
	}
}
