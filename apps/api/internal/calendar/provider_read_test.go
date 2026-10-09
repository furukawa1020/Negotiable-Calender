package calendar

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type countedCalendarBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *countedCalendarBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}
func (b *countedCalendarBody) Close() error { b.closed = true; return nil }

func TestCalendarDecodeClosesAndBoundsActualReads(t *testing.T) {
	for _, scenario := range []string{"declared-overflow", "unknown-length", "lying-length", "total-remainder", "cancelled", "trailing-json", "tiny-items", "duplicate-items", "duplicate-casing", "null-root"} {
		t.Run(scenario, func(t *testing.T) {
			budget := newCalendarReadBudget(false)
			budget.remainingBytes = 64
			data := strings.Repeat(" ", 128)
			length := int64(-1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "declared-overflow":
				length = 128
			case "lying-length":
				length = 1
			case "cancelled":
				cancel()
			case "trailing-json":
				data = `{} {"private":"secret"}`
			case "tiny-items":
				data = `{"items":[{},{},{}]}`
				budget.remainingItems = 2
			case "duplicate-items":
				data = `{"items":[],"items":[]}`
			case "duplicate-casing":
				data = `{"items":[],"ITEMS":[]}`
			case "null-root":
				data = `null`
			}
			body := &countedCalendarBody{reader: strings.NewReader(data)}
			var target struct{ Items []struct{} }
			err := budget.decode(ctx, &http.Response{Body: body, ContentLength: length}, &target)
			if err == nil || !body.closed || body.read > 65 || len(target.Items) != 0 {
				t.Fatalf("unbounded decode: read=%d closed=%v err=%v", body.read, body.closed, err)
			}
			if (scenario == "declared-overflow" || scenario == "cancelled") && body.read != 0 {
				t.Fatal("read despite early rejection")
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity lost")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("private content leaked")
			}
		})
	}
}

func TestCancelledCalendarReadMakesNoRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := NewGoogleProvider(GoogleConfig{}, &http.Client{Transport: limitTransport(func(*http.Request) (*http.Response, error) {
		t.Error("request made after cancellation")
		return nil, context.Canceled
	})})
	if _, err := provider.ListChanges(ctx, "access", "", time.Time{}, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := provider.ListPrivateEvents(ctx, "access", time.Time{}, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
