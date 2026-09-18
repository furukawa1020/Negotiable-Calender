package aiplanning

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type providerFunc func(context.Context, Payload) ([]string, error)

func (f providerFunc) Rank(ctx context.Context, payload Payload) ([]string, error) {
	return f(ctx, payload)
}

type permitFunc func(context.Context, Principal, string, time.Time) error

func (f permitFunc) Consume(ctx context.Context, principal Principal, token string, expires time.Time) error {
	return f(ctx, principal, token, expires)
}

var testPrincipal = Principal{"private-owner", "private-workspace"}
var testInfo = ProviderInfo{"synthetic-provider", "synthetic-model", "test-data-policy-v1"}

func fixture(t *testing.T) (*Planner, *Snapshot, *int, *Payload) {
	t.Helper()
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	s := &Snapshot{OwnerID: testPrincipal.UserID, OrganizationID: testPrincipal.OrganizationID, Revision: "private-source-revision", Active: true, ValidUntil: now.Add(30 * time.Minute), Candidates: []Candidate{
		{"private-candidate-A", now.Add(time.Hour), now.Add(90 * time.Minute)},
		{"private-candidate-B", now.Add(2 * time.Hour), now.Add(150 * time.Minute)},
	}}
	calls := new(int)
	sent := new(Payload)
	used := make(map[string]bool)
	p, err := New([]byte(strings.Repeat("k", 32)), testInfo, func(context.Context, Principal) (Snapshot, error) {
		copy := *s
		copy.Candidates = append([]Candidate(nil), s.Candidates...)
		return copy, nil
	}, providerFunc(func(_ context.Context, payload Payload) ([]string, error) {
		*calls++
		*sent = payload
		return []string{"c2", "c1"}, nil
	}), permitFunc(func(_ context.Context, principal Principal, token string, expires time.Time) error {
		if principal != testPrincipal || !expires.After(now) || used[token] {
			return errors.New("rejected")
		}
		used[token] = true
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return now }
	return p, s, calls, sent
}

func preview(t *testing.T, p *Planner) Preview {
	t.Helper()
	v, err := p.Preview(context.Background(), testPrincipal, Earlier)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConsentSendsOnlyPreviewedTimes(t *testing.T) {
	p, _, calls, sent := fixture(t)
	v := preview(t, p)
	if *calls != 0 {
		t.Fatal("preview called provider")
	}
	result, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true)
	if err != nil || *calls != 1 || len(result) != 2 || result[0].ID != "private-candidate-B" {
		t.Fatalf("result=%v error=%v calls=%d", result, err, *calls)
	}
	before, _ := json.Marshal(v.Payload)
	after, _ := json.Marshal(sent)
	if string(before) != string(after) {
		t.Fatal("sent payload differs from preview")
	}
	for _, secret := range []string{"private-", "title", "description", "attendees", "token", "workspace", "owner", "revision"} {
		if strings.Contains(string(after), secret) {
			t.Fatalf("payload includes forbidden field %s", secret)
		}
	}
	if v.Provider != testInfo.Name || v.Model != testInfo.Model || v.DataPolicyVersion != testInfo.DataPolicyVersion {
		t.Fatal("provider disclosure missing")
	}
	if _, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true); !errors.Is(err, ErrUnavailable) || *calls != 1 {
		t.Fatal("consent-use gate bypassed")
	}
}

func TestMissingOrTamperedConsentNeverCallsProvider(t *testing.T) {
	for _, name := range []string{"cancel", "empty", "tamper", "expired", "preference", "owner", "workspace", "provider", "model", "terms", "key"} {
		t.Run(name, func(t *testing.T) {
			p, _, calls, _ := fixture(t)
			v := preview(t, p)
			principal, pref, consent := testPrincipal, Earlier, true
			switch name {
			case "cancel":
				consent = false
			case "empty":
				v.ConsentToken = ""
			case "tamper":
				v.ConsentToken += "x"
			case "expired":
				p.now = func() time.Time { return v.ExpiresAt }
			case "preference":
				pref = Later
			case "owner":
				principal.UserID = "other-owner"
			case "workspace":
				principal.OrganizationID = "other-workspace"
			case "provider":
				p.info.Name = "other-provider"
			case "model":
				p.info.Model = "other-model"
			case "terms":
				p.info.DataPolicyVersion = "other-terms"
			case "key":
				p.key[0]++
			}
			if _, err := p.Generate(context.Background(), principal, pref, v.ConsentToken, consent); err == nil || *calls != 0 {
				t.Fatalf("err=%v calls=%d", err, *calls)
			}
		})
	}
}

func TestSourceChangesBeforeAndDuringInferenceAreRejected(t *testing.T) {
	mutations := map[string]func(*Snapshot){
		"deletion-or-disconnect": func(s *Snapshot) { s.Active = false },
		"revision":               func(s *Snapshot) { s.Revision = "new" },
		"candidate-time":         func(s *Snapshot) { s.Candidates[0].StartAt = s.Candidates[0].StartAt.Add(time.Minute) },
		"candidate-id":           func(s *Snapshot) { s.Candidates[0].ID = "changed" },
		"reordered":              func(s *Snapshot) { s.Candidates[0], s.Candidates[1] = s.Candidates[1], s.Candidates[0] },
		"expired-source":         func(s *Snapshot) { s.ValidUntil = time.Time{} },
		"owner":                  func(s *Snapshot) { s.OwnerID = "other" },
	}
	for name, mutate := range mutations {
		for _, during := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{true: "during", false: "before"}[during], func(t *testing.T) {
				p, s, calls, _ := fixture(t)
				v := preview(t, p)
				if during {
					p.provider = providerFunc(func(context.Context, Payload) ([]string, error) { *calls++; mutate(s); return []string{"c1"}, nil })
				} else {
					mutate(s)
				}
				if result, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true); err == nil || result != nil {
					t.Fatal("changed source accepted")
				}
				if !during && *calls != 0 {
					t.Fatal("stale data sent")
				}
			})
		}
	}
}

func TestPermitFailureAndChangesStopTransmission(t *testing.T) {
	for _, name := range []string{"quota", "source-change", "timeout", "cancelled-context"} {
		t.Run(name, func(t *testing.T) {
			p, s, calls, _ := fixture(t)
			v := preview(t, p)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.permit = permitFunc(func(context.Context, Principal, string, time.Time) error {
				switch name {
				case "quota":
					return errors.New("sensitive upstream message")
				case "source-change":
					s.Revision = "changed"
				case "timeout":
					p.now = func() time.Time { return v.ExpiresAt }
				case "cancelled-context":
					cancel()
				}
				return nil
			})
			_, err := p.Generate(ctx, testPrincipal, Earlier, v.ConsentToken, true)
			if err == nil || strings.Contains(err.Error(), "sensitive") || *calls != 0 {
				t.Fatalf("error=%v calls=%d", err, *calls)
			}
		})
	}
}

func TestInvalidModelOutputNeverBecomesProposal(t *testing.T) {
	for _, ids := range [][]string{nil, {}, {"c3"}, {"c1", "c1"}, {"private-candidate-A"}, {"c1", "c2", "c1", "c2"}, {"<script>alert(1)</script>"}} {
		p, _, _, _ := fixture(t)
		v := preview(t, p)
		p.provider = providerFunc(func(context.Context, Payload) ([]string, error) { return ids, nil })
		if _, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true); !errors.Is(err, ErrOutput) {
			t.Fatalf("accepted %v: %v", ids, err)
		}
	}
}

func TestProviderCannotChangeCandidateMapping(t *testing.T) {
	p, _, _, _ := fixture(t)
	v := preview(t, p)
	p.provider = providerFunc(func(_ context.Context, payload Payload) ([]string, error) {
		payload.Candidates[0].ID = "c2"
		return []string{"c2"}, nil
	})
	result, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true)
	if err != nil || result[0].ID != "private-candidate-B" {
		t.Fatal("provider changed trusted mapping")
	}
}

func TestFailureIsSanitizedAndNotRetried(t *testing.T) {
	p, _, calls, _ := fixture(t)
	v := preview(t, p)
	p.provider = providerFunc(func(context.Context, Payload) ([]string, error) {
		*calls++
		return nil, errors.New("secret token and calendar contents")
	})
	_, err := p.Generate(context.Background(), testPrincipal, Earlier, v.ConsentToken, true)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") || *calls != 1 {
		t.Fatalf("err=%v calls=%d", err, *calls)
	}
}

func TestPreviewRejectsInvalidSourcesAndClampsExpiry(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.Candidates = nil },
		func(s *Snapshot) { s.Revision = "" },
		func(s *Snapshot) { s.Candidates[0].ID = s.Candidates[1].ID },
		func(s *Snapshot) { s.Candidates[0].EndAt = s.Candidates[0].StartAt },
		func(s *Snapshot) { s.Candidates[0].StartAt = time.Time{} },
		func(s *Snapshot) { s.Candidates = make([]Candidate, 13) },
	} {
		p, s, _, _ := fixture(t)
		mutate(s)
		if _, err := p.Preview(context.Background(), testPrincipal, Earlier); !errors.Is(err, ErrSnapshot) {
			t.Fatal("invalid snapshot accepted")
		}
	}
	p, s, _, _ := fixture(t)
	s.ValidUntil = p.now().Add(10 * time.Second)
	if got := preview(t, p); !got.ExpiresAt.Equal(s.ValidUntil) {
		t.Fatal("consent outlives source")
	}
	if _, err := p.Preview(context.Background(), testPrincipal, Preference("send all data")); err == nil {
		t.Fatal("free text preference accepted")
	}
}

func TestNoPermissiveConfigurationDefaults(t *testing.T) {
	p, _, _, _ := fixture(t)
	for _, info := range []ProviderInfo{{}, {Name: "provider", Model: "model"}, {Name: "provider", DataPolicyVersion: "terms"}} {
		if _, err := New(p.key, info, p.load, p.provider, p.permit); err == nil {
			t.Fatal("missing disclosure accepted")
		}
	}
	if _, err := New(p.key, testInfo, p.load, p.provider, nil); err == nil {
		t.Fatal("missing budget gate accepted")
	}
	if _, err := New([]byte("short"), testInfo, p.load, p.provider, p.permit); err == nil {
		t.Fatal("weak signing key accepted")
	}
}
