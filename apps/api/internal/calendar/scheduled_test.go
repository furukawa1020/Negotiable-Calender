package calendar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/idtoken"
)

type batchStub struct {
	calls            int
	err              error
	result           BatchResult
	entered, release chan struct{}
}

func (s *batchStub) RunDue(ctx context.Context) (BatchResult, error) {
	s.calls++
	if s.entered != nil {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return s.result, ctx.Err()
		}
	}
	return s.result, s.err
}
func schedulerFixture(runner batchRunner, output io.Writer) *ScheduledHandler {
	h := NewScheduledHandler(http.NotFoundHandler(), runner, func() bool { return true }, ScheduledConfig{Audience: "https://example.test" + ScheduledSyncPath, Subject: "123456789012345678901", Email: "sync@example.iam.gserviceaccount.com"}, slog.New(slog.NewTextHandler(output, nil)))
	h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
		return validSchedulerPayload(h.config), nil
	}
	return h
}
func validSchedulerPayload(c ScheduledConfig) *idtoken.Payload {
	now := time.Now().Unix()
	return &idtoken.Payload{Issuer: "https://accounts.google.com", Audience: c.Audience, Subject: c.Subject, IssuedAt: now - 10, Expires: now + 300, Claims: map[string]any{"email": c.Email, "email_verified": true}}
}
func schedulerRequest() *http.Request {
	r := httptest.NewRequest("POST", ScheduledSyncPath, nil)
	r.Header.Set("Authorization", "Bearer synthetic")
	return r
}

func TestScheduledSyncRejectsUntrustedAuthority(t *testing.T) {
	for _, scenario := range []string{"anonymous", "demo", "cookie", "wrong-subject", "wrong-audience", "wrong-issuer", "wrong-email", "unverified", "expired", "future", "bad-signature", "duplicate-header", "body", "query", "disabled", "method"} {
		t.Run(scenario, func(t *testing.T) {
			s := &batchStub{}
			var logs bytes.Buffer
			h := schedulerFixture(s, &logs)
			r := schedulerRequest()
			want := 401
			p := validSchedulerPayload(h.config)
			h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
				if scenario == "bad-signature" {
					return nil, errors.New("secret-token")
				}
				return p, nil
			}
			switch scenario {
			case "anonymous":
				r.Header.Del("Authorization")
			case "demo":
				r.Header.Del("Authorization")
				r.Header.Set("X-Demo-User-ID", "admin")
			case "cookie":
				r.Header.Del("Authorization")
				r.AddCookie(&http.Cookie{Name: "negotiable_session", Value: "admin"})
			case "wrong-subject":
				p.Subject = "other"
			case "wrong-audience":
				p.Audience = "https://elsewhere.test"
			case "wrong-issuer":
				p.Issuer = "https://attacker.test"
			case "wrong-email":
				p.Claims["email"] = "user@example.test"
			case "unverified":
				p.Claims["email_verified"] = "true"
			case "expired":
				p.Expires = time.Now().Unix() - 1
			case "future":
				p.IssuedAt = time.Now().Add(time.Hour).Unix()
			case "duplicate-header":
				r.Header.Add("Authorization", "Bearer other")
			case "body":
				r.Body = io.NopCloser(strings.NewReader(`{"userId":"victim"}`))
				want = 400
			case "query":
				r.URL.RawQuery = "limit=10000"
				want = 400
			case "disabled":
				h.config = ScheduledConfig{}
				want = 404
			case "method":
				r.Method = "GET"
				want = 405
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want || s.calls != 0 {
				t.Fatalf("code=%d calls=%d", w.Code, s.calls)
			}
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(logs.String()+w.Body.String(), "secret-token") {
				t.Fatal("unsafe response/log")
			}
		})
	}
}

func TestScheduledSyncSummarizesFailuresWithoutSecrets(t *testing.T) {
	for _, scenario := range []string{"success", "claim", "timeout", "unconfigured"} {
		t.Run(scenario, func(t *testing.T) {
			s := &batchStub{result: BatchResult{Claimed: 2, Attempted: 2, Succeeded: 1, Failed: 1}}
			var logs bytes.Buffer
			h := schedulerFixture(s, &logs)
			want := 200
			state := "completed"
			if scenario == "claim" {
				s.err = errors.New("private-refresh-token")
				want = 503
				state = "claim_failed"
			}
			if scenario == "timeout" {
				s.err = context.DeadlineExceeded
				state = "budget_exhausted"
			}
			if scenario == "unconfigured" {
				h.configured = func() bool { return false }
				state = "not_configured"
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, schedulerRequest())
			if w.Code != want || !strings.Contains(w.Body.String(), state) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(logs.String()+w.Body.String(), "private-refresh-token") {
				t.Fatal("secret leak")
			}
			if scenario == "unconfigured" && s.calls != 0 {
				t.Fatal("unconfigured batch claimed work")
			}
		})
	}
}

func TestScheduledSyncRejectsOverlappingRequest(t *testing.T) {
	s := &batchStub{entered: make(chan struct{}), release: make(chan struct{})}
	h := schedulerFixture(s, io.Discard)
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(httptest.NewRecorder(), schedulerRequest()) }()
	<-s.entered
	w := httptest.NewRecorder()
	h.ServeHTTP(w, schedulerRequest())
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	close(s.release)
	<-done
	if s.calls != 1 {
		t.Fatal(s.calls)
	}
}

type observedBatch struct {
	batchRunner
	done chan struct{}
}

func (b observedBatch) RunDue(ctx context.Context) (BatchResult, error) {
	defer close(b.done)
	return b.batchRunner.RunDue(ctx)
}

func TestColdHTTPTriggerCompletesFakeCalendarSync(t *testing.T) {
	cipher := testCipher(t)
	encrypted, _ := cipher.Encrypt("synthetic-refresh")
	store := &backgroundStubStore{stubStore: stubStore{connection: Connection{UserID: "alice", RefreshTokenCipher: encrypted}}, claimed: []Connection{{UserID: "alice"}}}
	provider := &incrementalStubProvider{results: []ChangeSet{{Full: true, NextSyncToken: "next-cursor"}}}
	projector := &stubProjector{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncer := NewHandler(http.NotFoundHandler(), store, provider, cipher, projector, HandlerConfig{}, logger)
	worker := NewWorker(store, syncer, WorkerConfig{ClaimLimit: 5, SyncTimeout: time.Second}, logger)
	completed := make(chan struct{})
	server := httptest.NewServer(schedulerFixture(observedBatch{worker, completed}, io.Discard))
	defer server.Close()
	if store.claimCount != 0 || projector.rebuilt {
		t.Fatal("work started before request")
	}
	req, _ := http.NewRequest("POST", server.URL+ScheduledSyncPath, nil)
	req.Header.Set("Authorization", "Bearer synthetic")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	<-completed
	if res.StatusCode != 200 || !strings.Contains(string(body), `"succeeded":1`) || store.successToken != "next-cursor" || !projector.rebuilt {
		t.Fatalf("sync not complete at response: %s", body)
	}
}

type slowSyncer struct{ calls int }

func (s *slowSyncer) SyncUser(ctx context.Context, _ string) (SyncResult, error) {
	s.calls++
	<-ctx.Done()
	return SyncResult{}, ctx.Err()
}
func TestBatchBudgetAndClaimBounds(t *testing.T) {
	store := &backgroundStubStore{claimed: []Connection{{UserID: "one"}, {UserID: "two"}, {UserID: "three"}}}
	s := &slowSyncer{}
	w := NewWorker(store, s, WorkerConfig{ClaimLimit: 5, RunTimeout: 10 * time.Millisecond, SyncTimeout: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := w.RunDue(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || result.Attempted != 1 || result.Unprocessed != 2 || result.Failed != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := store.claimCount
	if _, err := w.RunDue(ctx); !errors.Is(err, context.Canceled) || store.claimCount != before {
		t.Fatal("cancelled batch still claimed")
	}
	w.config.ClaimLimit = 1
	if _, err := w.RunDue(context.Background()); err == nil {
		t.Fatal("oversized claim accepted")
	}
}
