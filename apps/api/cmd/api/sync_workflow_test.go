package main

import (
	"os"
	"strings"
	"testing"
)

func TestScheduledWorkflowUsesExplicitEmptyPost(t *testing.T) {
	data, err := os.ReadFile("../../../../.github/workflows/calendar-sync.yml")
	if err != nil {
		t.Fatal(err)
	}
	// Cloud Run's frontend rejects an unframed POST with 411 before our handler.
	for _, invariant := range []string{"--request POST", "--header 'Content-Length: 0'", "--max-time 55 --retry 0", "vars.CALENDAR_SYNC_ENABLED == 'true'", "token_format: id_token", "id_token_audience: ${{ env.SYNC_URL }}"} {
		if !strings.Contains(string(data), invariant) {
			t.Errorf("scheduled request invariant missing: %s", invariant)
		}
	}
}
