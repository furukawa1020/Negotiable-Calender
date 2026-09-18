// Package aiplanning provides an opt-in proposal boundary. It is not wired to
// production: callers must supply authenticated storage and a durable zero-cost
// quota/consent-use gate before enabling external inference.
package aiplanning

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrConsent     = errors.New("AI consent missing, changed or expired")
	ErrSnapshot    = errors.New("AI scheduling source unavailable or stale")
	ErrUnavailable = errors.New("AI unavailable; use standard scheduling")
	ErrOutput      = errors.New("AI returned an invalid proposal")
)

const PolicyVersion = "candidate-times-only-v1"
const maxCandidates = 12

// ProviderInfo is immutable server configuration included in every consent.
// Changing provider, model or data terms requires a fresh preview and consent.
type ProviderInfo struct{ Name, Model, DataPolicyVersion string }

type Principal struct{ UserID, OrganizationID string }
type Candidate struct {
	ID             string
	StartAt, EndAt time.Time
}

// Snapshot must come from authorized server storage, not client JSON. Revision
// covers source, sharing policy, request and booking state. Active includes the
// deletion/disconnection fences. Candidates must already pass the existing
// deterministic availability/booking validation.
type Snapshot struct {
	OwnerID, OrganizationID, Revision string
	Active                            bool
	ValidUntil                        time.Time
	Candidates                        []Candidate
}
type Loader func(context.Context, Principal) (Snapshot, error)

type Preference string

const (
	Earlier Preference = "earlier"
	Later   Preference = "later"
)

// Payload deliberately cannot contain names, titles, descriptions, attendees,
// calendar IDs, user IDs, workspace IDs, tokens or free-text instructions.
type Payload struct {
	Preference Preference      `json:"preference"`
	Candidates []WireCandidate `json:"candidates"`
}
type WireCandidate struct {
	ID      string    `json:"id"`
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}
type Preview struct {
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	PolicyVersion     string    `json:"policyVersion"`
	DataPolicyVersion string    `json:"dataPolicyVersion"`
	Payload           Payload   `json:"payload"`
	ExpiresAt         time.Time `json:"expiresAt"`
	ConsentToken      string    `json:"consentToken"`
}
type Provider interface {
	Rank(context.Context, Payload) ([]string, error)
}

// Permit must atomically verify zero-cost eligibility, reserve account/user budget and
// consume the consent token once. Failed inference must not refund this use:
// inference may already have consumed quota. No permissive default is supplied.
type Permit interface {
	Consume(context.Context, Principal, string, time.Time) error
}

type Planner struct {
	key      []byte
	info     ProviderInfo
	load     Loader
	provider Provider
	permit   Permit
	now      func() time.Time
}

func New(key []byte, info ProviderInfo, load Loader, provider Provider, permit Permit) (*Planner, error) {
	if len(key) < 32 || load == nil || provider == nil || permit == nil {
		return nil, ErrUnavailable
	}
	for _, value := range []string{info.Name, info.Model, info.DataPolicyVersion} {
		if strings.TrimSpace(value) == "" || len(value) > 256 {
			return nil, ErrUnavailable
		}
	}
	return &Planner{key: append([]byte(nil), key...), info: info, load: load, provider: provider, permit: permit, now: time.Now}, nil
}

func (p *Planner) prepare(ctx context.Context, principal Principal, preference Preference) (Snapshot, Payload, string, error) {
	if principal.UserID == "" || principal.OrganizationID == "" {
		return Snapshot{}, Payload{}, "", ErrSnapshot
	}
	if preference != Earlier && preference != Later {
		return Snapshot{}, Payload{}, "", ErrSnapshot
	}
	s, err := p.load(ctx, principal)
	now := p.now().UTC()
	if err != nil || !s.Active || s.OwnerID != principal.UserID || s.OrganizationID != principal.OrganizationID || s.Revision == "" || !s.ValidUntil.After(now) || len(s.Candidates) == 0 || len(s.Candidates) > maxCandidates {
		return Snapshot{}, Payload{}, "", ErrSnapshot
	}
	payload := Payload{Preference: preference, Candidates: make([]WireCandidate, len(s.Candidates))}
	seen := make(map[string]bool)
	for i, c := range s.Candidates {
		if c.ID == "" || seen[c.ID] || !c.StartAt.After(now) || !c.EndAt.After(c.StartAt) || c.EndAt.Sub(c.StartAt) > 24*time.Hour {
			return Snapshot{}, Payload{}, "", ErrSnapshot
		}
		seen[c.ID] = true
		payload.Candidates[i] = WireCandidate{ID: fmt.Sprintf("c%d", i+1), StartAt: c.StartAt.UTC(), EndAt: c.EndAt.UTC()}
	}
	// Internal IDs are only hashed locally; never sent to the provider.
	encoded, err := json.Marshal(struct {
		Snapshot Snapshot
		Payload  Payload
		Policy   string
		Provider ProviderInfo
	}{s, payload, PolicyVersion, p.info})
	if err != nil {
		return Snapshot{}, Payload{}, "", ErrSnapshot
	}
	hash := sha256.Sum256(encoded)
	return s, payload, hex.EncodeToString(hash[:]), nil
}

func (p *Planner) Preview(ctx context.Context, principal Principal, preference Preference) (Preview, error) {
	s, payload, digest, err := p.prepare(ctx, principal, preference)
	if err != nil {
		return Preview{}, err
	}
	expires := p.now().UTC().Add(2 * time.Minute)
	if s.ValidUntil.Before(expires) {
		expires = s.ValidUntil
	}
	expires = expires.Truncate(time.Second)
	if !expires.After(p.now()) {
		return Preview{}, ErrSnapshot
	}
	claim := digest + "." + strconv.FormatInt(expires.Unix(), 10)
	token := claim + "." + p.sign(claim)
	return Preview{Provider: p.info.Name, Model: p.info.Model, PolicyVersion: PolicyVersion, DataPolicyVersion: p.info.DataPolicyVersion, Payload: payload, ExpiresAt: expires, ConsentToken: token}, nil
}

func (p *Planner) sign(value string) string {
	h := hmac.New(sha256.New, p.key)
	h.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// Generate returns only existing server candidates, never model-generated text
// or executable actions. Reservation still requires human confirmation through
// the existing transactional confirmation engine.
func (p *Planner) Generate(ctx context.Context, principal Principal, preference Preference, token string, consent bool) ([]Candidate, error) {
	if !consent || len(token) > 256 {
		return nil, ErrConsent
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[0]) != 64 || !hmac.Equal([]byte(parts[2]), []byte(p.sign(parts[0]+"."+parts[1]))) {
		return nil, ErrConsent
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	expires := time.Unix(unix, 0)
	if err != nil || !expires.After(p.now()) || expires.After(p.now().Add(2*time.Minute)) {
		return nil, ErrConsent
	}
	_, payload, digest, err := p.prepare(ctx, principal, preference)
	if err != nil {
		return nil, err
	}
	if digest != parts[0] {
		return nil, ErrConsent
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrUnavailable
	}
	if err := p.permit.Consume(ctx, principal, token, expires); err != nil {
		return nil, ErrUnavailable
	}
	// The atomic permit can take time. Recheck before disclosing anything.
	_, payload, freshDigest, err := p.prepare(ctx, principal, preference)
	if err != nil || digest != freshDigest || !expires.After(p.now()) {
		return nil, ErrConsent
	}
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	ids, err := p.provider.Rank(ctx, payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	s, payload, afterDigest, err := p.prepare(ctx, principal, preference)
	if err != nil || digest != afterDigest || !expires.After(p.now()) {
		return nil, ErrConsent
	}
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	if len(ids) == 0 || len(ids) > 3 {
		return nil, ErrOutput
	}
	seen := make(map[string]bool)
	result := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, ErrOutput
		}
		seen[id] = true
		found := false
		for i, wire := range payload.Candidates {
			if id == wire.ID {
				result = append(result, s.Candidates[i])
				found = true
				break
			}
		}
		if !found {
			return nil, ErrOutput
		}
	}
	return result, nil
}
