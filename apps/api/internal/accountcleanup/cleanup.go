// Package accountcleanup coordinates bounded retries of already authorized deletions.
package accountcleanup

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid cleanup options")
var ErrIncomplete = errors.New("cleanup incomplete; inspect counts and retry pending accounts")

type Page struct {
	UserIDs []string
	HasMore bool
}

type Store interface {
	ListPendingAccountDeletions(context.Context, int, string) (Page, error)
	ResumeAccountDeletion(context.Context, string) error
}

type Options struct {
	User                       string
	Pending, DryRun            bool
	Limit                      int
	Cursor                     string
	Timeout, PerAccountTimeout time.Duration
}

// Cursor is intentionally excluded from JSON: it encodes a pseudonymous user ID.
type Result struct {
	Selected      int    `json:"selected"`
	Attempted     int    `json:"attempted"`
	Succeeded     int    `json:"succeeded"`
	Failed        int    `json:"failed"`
	Remaining     int    `json:"remaining"`
	HasMore       bool   `json:"hasMore"`
	Interrupted   bool   `json:"interrupted"`
	ListingFailed bool   `json:"listingFailed"`
	DryRun        bool   `json:"dryRun"`
	NextCursor    string `json:"-"`
}

func ValidUserID(id string) bool {
	return id != "" && len(id) <= 1500 && utf8.ValidString(id) && strings.TrimSpace(id) == id &&
		id != "." && id != ".." && !strings.ContainsRune(id, '/') && !strings.ContainsRune(id, rune(92)) &&
		!(strings.HasPrefix(id, "__") && strings.HasSuffix(id, "__")) &&
		strings.IndexFunc(id, unicode.IsControl) < 0
}

func EncodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("pending-v1:" + id))
}

func DecodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) > 2048 {
		return "", ErrInvalid
	}
	value, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(value), "pending-v1:") {
		return "", ErrInvalid
	}
	id := strings.TrimPrefix(string(value), "pending-v1:")
	if !ValidUserID(id) {
		return "", ErrInvalid
	}
	return id, nil
}

func (o Options) Validate() error {
	if o.Pending == (o.User != "") || (!o.Pending && !ValidUserID(o.User)) ||
		o.Limit < 1 || o.Limit > 100 || o.Timeout <= 0 || o.Timeout > 15*time.Minute ||
		o.PerAccountTimeout <= 0 || o.PerAccountTimeout > 5*time.Minute ||
		(!o.Pending && (o.Cursor != "" || o.DryRun)) {
		return ErrInvalid
	}
	_, err := DecodeCursor(o.Cursor)
	return err
}

func Run(parent context.Context, store Store, options Options) (Result, error) {
	result := Result{DryRun: options.DryRun}
	if err := options.Validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	page := Page{UserIDs: []string{options.User}}
	if options.Pending {
		var err error
		page, err = store.ListPendingAccountDeletions(ctx, options.Limit, options.Cursor)
		if err != nil {
			result.ListingFailed = true
			result.Interrupted = ctx.Err() != nil
			result.HasMore = true
			result.NextCursor = options.Cursor
			return result, ErrIncomplete
		}
	}
	// Fail before any mutation if a storage implementation breaks the page contract.
	last, _ := DecodeCursor(options.Cursor)
	if len(page.UserIDs) > options.Limit || (page.HasMore && len(page.UserIDs) == 0) {
		return result, ErrInvalid
	}
	for _, id := range page.UserIDs {
		if !ValidUserID(id) || (options.Pending && id <= last) {
			return result, ErrInvalid
		}
		last = id
	}
	result.Selected = len(page.UserIDs)
	result.HasMore = page.HasMore
	result.NextCursor = options.Cursor
	if options.DryRun {
		if len(page.UserIDs) > 0 {
			result.NextCursor = EncodeCursor(last)
		}
		if !result.HasMore {
			result.NextCursor = ""
		}
		return result, nil
	}
	for i, id := range page.UserIDs {
		if ctx.Err() != nil {
			result.Interrupted = true
			result.Remaining = len(page.UserIDs) - i
			result.HasMore = true
			return result, ErrIncomplete
		}
		accountCtx, accountCancel := context.WithTimeout(ctx, options.PerAccountTimeout)
		err := store.ResumeAccountDeletion(accountCtx, id)
		accountCancel()
		result.Attempted++
		if err != nil {
			result.Failed++
		} else {
			result.Succeeded++
		}
		if options.Pending {
			result.NextCursor = EncodeCursor(id)
		}
	}
	if !result.HasMore {
		result.NextCursor = ""
	}
	result.Interrupted = ctx.Err() != nil
	if result.Failed > 0 || result.Interrupted {
		return result, ErrIncomplete
	}
	return result, nil
}
