package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/aiplanning"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

// This read-only preview is not a reservation or an external AI authorization.
// Its fingerprint detects changes between local inference and result display;
// final acceptance still uses the transactional confirmation engine.
type planningPreview struct {
	Revision   string                     `json:"revision"`
	ExpiresAt  time.Time                  `json:"expiresAt"`
	Candidates []aiplanning.WireCandidate `json:"candidates"`
}

func (api *API) getPlanningPreview(w http.ResponseWriter, r *http.Request) {
	userID, organizationID := r.Header.Get("X-Demo-User-ID"), r.Header.Get("X-Organization-ID")
	if userID == "" || organizationID == "" {
		writeJSON(w, 401, map[string]string{"error": "request identity is required"})
		return
	}
	if allowed, retry := api.planningBudget.allow(userID, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "planning request limit reached"})
		return
	}
	sources, supported := api.requests.(planningSourceStore)
	if !supported {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bounded planning unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	value, err := api.requests.GetForUser(r.Context(), r.PathValue("requestId"), userID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "request not found"})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "planning preview unavailable"})
		return
	}
	if value.TargetUserID != userID || value.OrganizationID != organizationID {
		writeJSON(w, 403, map[string]string{"error": "only the target in the active workspace can preview"})
		return
	}
	if !api.requireMembership(w, r, organizationID, userID) {
		return
	}
	now := time.Now().UTC()
	if value.Status != coordinationrequest.Suggested || !value.DeadlineAt.After(now) || len(value.Options) > 12 {
		writeJSON(w, 409, map[string]string{"error": "request is not eligible for planning"})
		return
	}
	options := []coordinationrequest.Option{}
	for _, option := range value.Options {
		if option.Type != coordinationrequest.OptionMeeting {
			continue
		}
		valid, err := coordinationrequest.ConfirmableMeeting(value, option.ID, now)
		if err == nil {
			options = append(options, valid)
		}
	}
	if len(options) == 0 {
		writeJSON(w, 409, map[string]string{"error": "no current meeting candidates"})
		return
	}
	from, to := *options[0].StartAt, *options[0].EndAt
	for _, option := range options {
		if option.StartAt.Before(from) {
			from = *option.StartAt
		}
		if option.EndAt.After(to) {
			to = *option.EndAt
		}
	}
	if to.Sub(from) > 31*24*time.Hour {
		writeJSON(w, 409, map[string]string{"error": "planning range too large"})
		return
	}
	segments, bookings, err := sources.LoadPlanningSources(ctx, userID, value.RequesterUserID, from, to)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "bounded planning sources cannot be verified"})
		return
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].ID < segments[j].ID })
	sort.Slice(bookings, func(i, j int) bool { return bookings[i].ID < bookings[j].ID })
	preview := planningPreview{ExpiresAt: now.Add(2 * time.Minute), Candidates: []aiplanning.WireCandidate{}}
	if value.DeadlineAt.Before(preview.ExpiresAt) {
		preview.ExpiresAt = value.DeadlineAt
	}
	for _, option := range options {
		if coordinationrequest.ValidateMeetingAvailability(userID, option, segments, now) != nil {
			continue
		}
		conflict := false
		for _, booking := range bookings {
			if coordinationrequest.ConflictsWithMeeting(option, booking) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		preview.Candidates = append(preview.Candidates, aiplanning.WireCandidate{ID: fmt.Sprintf("c%d", len(preview.Candidates)+1), StartAt: *option.StartAt, EndAt: *option.EndAt})
		if option.StartAt.Before(preview.ExpiresAt) {
			preview.ExpiresAt = *option.StartAt
		}
		for _, segment := range segments {
			if segment.StartAt.Before(*option.EndAt) && option.StartAt.Before(segment.EndAt) && segment.ExpiresAt.Before(preview.ExpiresAt) {
				preview.ExpiresAt = segment.ExpiresAt
			}
		}
	}
	if len(preview.Candidates) == 0 || !preview.ExpiresAt.After(now) {
		writeJSON(w, 409, map[string]string{"error": "availability or bookings changed"})
		return
	}
	// Hash only; titles, participant IDs and internal state are never sent to AI.
	encoded, err := json.Marshal([]any{value, segments, bookings, preview.Candidates})
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "planning preview unavailable"})
		return
	}
	digest := sha256.Sum256(encoded)
	preview.Revision = hex.EncodeToString(digest[:])
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, preview)
}
