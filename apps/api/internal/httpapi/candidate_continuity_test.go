package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coord "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestCreateLongRequestFromRawProjectionBuckets(t *testing.T) {
	for _, minutes := range []int{30, 60} {
		for _, reserved := range []bool{false, true} {
			t.Run(fmt.Sprintf("minutes-%d-reserved-%v", minutes, reserved), func(t *testing.T) {
				now := time.Now().UTC()
				start := now.Truncate(time.Hour).Add(2 * time.Hour)
				end := start.Add(3 * time.Hour)
				projections := &stubProjectionStore{}
				for i := range 12 {
					at := start.Add(time.Duration(i) * projection.BucketSize)
					projections.values = append(projections.values, projection.ScheduleProjection{
						ID: fmt.Sprintf("bucket-%d", i), UserID: "manager-1", StartAt: at, EndAt: at.Add(projection.BucketSize),
						State:       policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh},
						GeneratedAt: now, ExpiresAt: now.Add(time.Hour), ExpectedResponseBucket: "soon",
					})
				}
				store := &stubRequestStore{}
				busyStart, busyEnd := start.Add(30*time.Minute), start.Add(time.Hour)
				if reserved {
					store.values = []coord.CoordinationRequest{{ID: "existing", OrganizationID: "other-org", RequesterUserID: "manager-1", TargetUserID: "peer", Status: coord.Accepted, AcceptedOptionID: "chosen",
						Options: []coord.Option{{ID: "chosen", RequestID: "existing", Type: coord.OptionMeeting, StartAt: &busyStart, EndAt: &busyEnd}}}}
				}
				handler := New(stubDatabase{}, &stubPolicyStore{}, projections, &stubOrganizationStore{}, store, "", testLogger())
				body, _ := json.Marshal(map[string]any{"targetUserId": "manager-1", "type": "meeting", "title": "Synthetic long meeting", "durationMinutes": minutes, "deadlineAt": end, "syncPreference": "sync", "priority": "normal"})
				req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
				req.Header.Set("X-Demo-User-ID", "member-1")
				req.Header.Set("X-Organization-ID", "org-1")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != http.StatusCreated {
					t.Fatalf("status=%d body=%s", response.Code, response.Body)
				}
				var created coord.CoordinationRequest
				if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
					t.Fatal(err)
				}
				if len(created.Options) != 3 || len(store.value.Options) != 3 || created.Status != coord.Suggested {
					t.Fatalf("candidate creation missing: %+v", created)
				}
				for _, option := range created.Options {
					if option.Type != coord.OptionMeeting || option.StartAt == nil || option.EndAt == nil || option.EndAt.Sub(*option.StartAt) != time.Duration(minutes)*time.Minute {
						t.Fatalf("invalid long candidate: %+v", option)
					}
					if option.EndAt.After(end) || (reserved && option.StartAt.Before(busyEnd) && busyStart.Before(*option.EndAt)) {
						t.Fatalf("deadline/reservation crossed: %+v", option)
					}
					if err := coord.ValidateMeetingAvailability("manager-1", option, projections.values, now); err != nil {
						t.Fatal(err)
					}
				}
				if strings.Contains(response.Body.String(), "score") {
					t.Fatal("ranking score leaked")
				}
			})
		}
	}
}
