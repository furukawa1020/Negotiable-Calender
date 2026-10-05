package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCreationExpiryReturnsActionableConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeCreationError(w, fmt.Errorf("private storage detail: %w", coord.ErrCreationExpired))
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || body["code"] != "creation_expired" || strings.Contains(w.Body.String(), "private storage") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
