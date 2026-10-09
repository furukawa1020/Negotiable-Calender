package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Application safeguards, not provider quotas. Raw items include cancellations
// and skipped private-view records; otherwise empty pages could bypass limits.
const (
	calendarPageLimit    = 20
	calendarPageBytes    = 2 * 1024 * 1024
	calendarCursorBytes  = 16 * 1024
	calendarSyncBytes    = 16 * 1024 * 1024
	calendarPrivateBytes = 8 * 1024 * 1024
	calendarSyncItems    = 10000
	calendarPrivateItems = 1000
)

var errCalendarReadLimit = errors.New("calendar response limit exceeded")
var errCalendarResponse = errors.New("invalid calendar response")

type calendarReadBudget struct {
	remainingBytes int64
	remainingItems int
	pages          int
	seen           map[string]bool
}

func newCalendarReadBudget(private bool) *calendarReadBudget {
	budget := &calendarReadBudget{remainingBytes: calendarSyncBytes, remainingItems: calendarSyncItems, seen: make(map[string]bool)}
	if private {
		budget.remainingBytes, budget.remainingItems = calendarPrivateBytes, calendarPrivateItems
	}
	return budget
}

func (b *calendarReadBudget) beginPage(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.pages >= calendarPageLimit || b.remainingBytes <= 0 || len(token) > calendarCursorBytes || b.seen[token] {
		return errCalendarReadLimit
	}
	b.pages++
	b.seen[token] = true
	return nil
}

// The cap is enforced on bytes actually read, including chunked/decompressed
// bodies, with one sentinel byte. Even early failures always close the response.
func (b *calendarReadBudget) decode(ctx context.Context, response *http.Response, target any) error {
	defer response.Body.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	limit := min(int64(calendarPageBytes), b.remainingBytes)
	if limit <= 0 || response.ContentLength > limit {
		return errCalendarReadLimit
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errCalendarResponse
	}
	if int64(len(data)) > limit {
		return errCalendarReadLimit
	}
	b.remainingBytes -= int64(len(data))
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errCalendarResponse
	}
	// Check raw item count before allocating the much larger typed item slice.
	// A tiny-object array must not amplify a bounded JSON body into an unbounded
	// allocation. The raw envelope is itself limited by the byte budget above.
	if !json.Valid(trimmed) {
		return errCalendarResponse
	}
	fields := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := fields.Token(); err != nil {
		return errCalendarResponse
	}
	seenItems := false
	for fields.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		key, err := fields.Token()
		if err != nil {
			return errCalendarResponse
		}
		var raw json.RawMessage
		if fields.Decode(&raw) != nil {
			return errCalendarResponse
		}
		if !strings.EqualFold(key.(string), "items") {
			continue
		}
		// Reject duplicate keys, including casing variants: checking only the
		// final array would let an earlier oversized array allocate first.
		if seenItems {
			return errCalendarResponse
		}
		seenItems = true
		items := bytes.TrimSpace(raw)
		if bytes.Equal(items, []byte("null")) {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(items))
		token, err := decoder.Token()
		if err != nil || token != json.Delim('[') {
			return errCalendarResponse
		}
		count := 0
		for decoder.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > b.remainingItems {
				return errCalendarReadLimit
			}
			var item json.RawMessage
			if decoder.Decode(&item) != nil {
				return errCalendarResponse
			}
		}
	}
	if json.Unmarshal(trimmed, target) != nil {
		return errCalendarResponse
	}
	return ctx.Err()
}

func (b *calendarReadBudget) account(items int, cursors ...string) error {
	if items > b.remainingItems {
		return errCalendarReadLimit
	}
	for _, cursor := range cursors {
		if len(cursor) > calendarCursorBytes {
			return errCalendarReadLimit
		}
	}
	b.remainingItems -= items
	return nil
}
