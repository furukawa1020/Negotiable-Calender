package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type stubDatabase struct {
	err error
}

type stubPolicyStore struct {
	value     policy.SharingPolicy
	err       error
	overrides []policy.ManualOverride
}

type stubProjectionStore struct {
	view   projection.View
	values []projection.ScheduleProjection
	err    error
}

func (store *stubProjectionStore) List(context.Context, string, time.Time, time.Time) ([]projection.ScheduleProjection, error) {
	return store.values, store.err
}

func (store *stubProjectionStore) ListForUser(context.Context, string) ([]projection.ScheduleProjection, error) {
	return store.values, store.err
}

type stubOrganizationStore struct {
	people  []organization.Person
	members map[string]bool
	err     error
}

type stubRequestStore struct {
	value           coordinationrequest.CoordinationRequest
	values          []coordinationrequest.CoordinationRequest
	target          string
	respondID       string
	respondTarget   string
	respondStatus   coordinationrequest.Status
	respondOption   string
	getUser         string
	cancelledID     string
	cancelledUser   string
	getErr          error
	cancelErr       error
	suggestedOption coordinationrequest.Option
	delegatedOption coordinationrequest.Option
	err             error
}

type stubNotificationStore struct {
	values     []notification.Notification
	listUser   string
	readID     string
	readUser   string
	readResult bool
	err        error
}

type stubAuditStore struct {
	values           []audit.Event
	listOrganization string
	err              error
}

func (store *stubAuditStore) Create(_ context.Context, value audit.Event) error {
	store.values = append(store.values, value)
	return store.err
}

func (store *stubAuditStore) List(_ context.Context, organizationID string) ([]audit.Event, error) {
	store.listOrganization = organizationID
	return store.values, store.err
}

func (store *stubNotificationStore) Create(_ context.Context, value notification.Notification) error {
	store.values = append(store.values, value)
	return store.err
}

func (store *stubNotificationStore) List(_ context.Context, userID string) ([]notification.Notification, error) {
	store.listUser = userID
	return store.values, store.err
}

func (store *stubNotificationStore) MarkRead(_ context.Context, id, userID string, _ time.Time) (bool, error) {
	store.readID, store.readUser = id, userID
	return store.readResult, store.err
}

func (store *stubRequestStore) Delegate(_ context.Context, requestID, targetUserID string, option coordinationrequest.Option) error {
	store.respondID, store.respondTarget = requestID, targetUserID
	store.delegatedOption = option
	return store.err
}

func (store *stubRequestStore) Suggest(_ context.Context, requestID, targetUserID string, option coordinationrequest.Option) error {
	store.respondID, store.respondTarget = requestID, targetUserID
	store.suggestedOption = option
	return store.err
}

func (store *stubRequestStore) Respond(_ context.Context, requestID, targetUserID string, status coordinationrequest.Status, optionID string) error {
	store.respondID, store.respondTarget = requestID, targetUserID
	store.respondStatus, store.respondOption = status, optionID
	return store.err
}

func (store *stubRequestStore) Create(_ context.Context, value coordinationrequest.CoordinationRequest) error {
	store.value = value
	return store.err
}

func (store *stubRequestStore) ListForTarget(_ context.Context, targetUserID string) ([]coordinationrequest.CoordinationRequest, error) {
	store.target = targetUserID
	return store.values, store.err
}

func (store *stubRequestStore) ListForRequester(_ context.Context, requesterUserID string) ([]coordinationrequest.CoordinationRequest, error) {
	store.target = requesterUserID
	return store.values, store.err
}

func (store *stubRequestStore) ListForUser(_ context.Context, userID string) ([]coordinationrequest.CoordinationRequest, error) {
	store.target = userID
	return store.values, store.err
}

func (store *stubRequestStore) GetForUser(_ context.Context, _ string, userID string) (coordinationrequest.CoordinationRequest, error) {
	store.getUser = userID
	if store.getErr != nil {
		return coordinationrequest.CoordinationRequest{}, store.getErr
	}
	return store.value, store.err
}

func (store *stubRequestStore) Cancel(_ context.Context, requestID, requesterUserID string) error {
	store.cancelledID, store.cancelledUser = requestID, requesterUserID
	if store.cancelErr != nil {
		return store.cancelErr
	}
	return store.err
}

func (store *stubOrganizationStore) ListPeople(context.Context, string) ([]organization.Person, error) {
	return store.people, store.err
}

func (store *stubOrganizationStore) IsMember(_ context.Context, organizationID, userID string) (bool, error) {
	if store.err != nil {
		return false, store.err
	}
	if store.members == nil {
		return true, nil
	}
	return store.members[organizationID+":"+userID], nil
}

func (store *stubProjectionStore) GetView(context.Context, string, string, time.Time, time.Time) (projection.View, error) {
	return store.view, store.err
}

func (store *stubPolicyStore) ListActiveOverrides(context.Context, string, time.Time) ([]policy.ManualOverride, error) {
	return store.overrides, store.err
}

func (store *stubPolicyStore) CreateOverride(_ context.Context, value policy.ManualOverride) error {
	store.overrides = append(store.overrides, value)
	return store.err
}

func (store *stubPolicyStore) Get(context.Context, string) (policy.SharingPolicy, error) {
	return store.value, store.err
}

func (store *stubPolicyStore) Upsert(_ context.Context, value policy.SharingPolicy) error {
	store.value = value
	store.err = nil
	return nil
}

func (database stubDatabase) PingContext(context.Context) error {
	return database.err
}

func TestHealth(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestReadinessFailsClosedWhenDatabaseIsUnavailable(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	New(stubDatabase{err: errors.New("database unavailable")}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
	if strings.Contains(response.Body.String(), "database unavailable") {
		t.Fatal("internal database error leaked to response")
	}
	if !strings.Contains(response.Body.String(), `"status":"unavailable"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestCORSAllowsOnlyConfiguredWebOrigin(t *testing.T) {
	t.Parallel()

	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "https://calendar.example", testLogger())
	for _, test := range []struct {
		name           string
		origin         string
		expectedHeader string
	}{
		{name: "configured origin", origin: "https://calendar.example", expectedHeader: "https://calendar.example"},
		{name: "other origin", origin: "https://attacker.example", expectedHeader: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if actual := response.Header().Get("Access-Control-Allow-Origin"); actual != test.expectedHeader {
				t.Fatalf("expected origin %q, got %q", test.expectedHeader, actual)
			}
		})
	}
}

func TestCORSPreflightAllowsSharingPolicyUpdate(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "https://calendar.example", testLogger())
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/users/manager-1/sharing-policy", nil)
	request.Header.Set("Origin", "https://calendar.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("PUT missing from CORS methods: %q", response.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestSharingPolicyPutThenGet(t *testing.T) {
	t.Parallel()
	store := &stubPolicyStore{err: policy.ErrNotFound}
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	body := `{
  "default": {
    "availability": "available",
    "interruptibility": "normal",
    "requestability": "open",
    "reschedulability": "medium"
  },
  "workingHours": [{"weekday": 1, "startMinute": 540, "endMinute": 1080}],
  "rules": [{
    "conditionType": "organization",
    "condition": {},
    "state": {
      "availability": "limited",
      "interruptibility": "urgent_only",
      "requestability": "later",
      "reschedulability": "low"
    },
    "priority": 10,
    "enabled": true
  }]
}`
	put := httptest.NewRequest(http.MethodPut, "/api/v1/users/manager-1/sharing-policy", strings.NewReader(body))
	put.Header.Set("X-Demo-User-ID", "manager-1")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("expected PUT 200, got %d: %s", putResponse.Code, putResponse.Body)
	}
	if store.value.UserID != "manager-1" {
		t.Fatalf("path user was not persisted: %q", store.value.UserID)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/sharing-policy", nil)
	get.Header.Set("X-Demo-User-ID", "manager-1")
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected GET 200, got %d", getResponse.Code)
	}
	for _, expected := range []string{"workingHours", "interruptibility", "requestability", "reschedulability"} {
		if !strings.Contains(getResponse.Body.String(), expected) {
			t.Fatalf("response missing %q: %s", expected, getResponse.Body)
		}
	}
}

func TestSharingPolicyRejectsInvalidState(t *testing.T) {
	t.Parallel()
	store := &stubPolicyStore{err: policy.ErrNotFound}
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	body := `{"default":{"availability":"secret_meeting"},"workingHours":[],"rules":[]}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/manager-1/sharing-policy", strings.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", response.Code, response.Body)
	}
}

func TestSharingPolicyGetReturnsNotFound(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{err: policy.ErrNotFound}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/missing/sharing-policy", nil)
	request.Header.Set("X-Demo-User-ID", "missing")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}

func TestManualOverrideCreateThenList(t *testing.T) {
	t.Parallel()
	store := &stubPolicyStore{}
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	now := time.Now().UTC()
	body, err := json.Marshal(map[string]any{
		"startAt":   now.Add(time.Hour),
		"endAt":     now.Add(2 * time.Hour),
		"expiresAt": now.Add(3 * time.Hour),
		"state": map[string]string{
			"availability":     "limited",
			"interruptibility": "urgent_only",
			"requestability":   "later",
			"reschedulability": "low",
		},
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/v1/users/manager-1/manual-overrides", bytes.NewReader(body))
	post.Header.Set("X-Demo-User-ID", "manager-1")
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusCreated {
		t.Fatalf("expected POST 201, got %d: %s", postResponse.Code, postResponse.Body)
	}
	if len(store.overrides) != 1 {
		t.Fatalf("override was not persisted")
	}
	if store.overrides[0].UserID != "manager-1" {
		t.Fatalf("path user was not persisted")
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/manual-overrides", nil)
	get.Header.Set("X-Demo-User-ID", "manager-1")
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected GET 200, got %d", getResponse.Code)
	}
	if !strings.Contains(getResponse.Body.String(), "urgent_only") {
		t.Fatalf("stored override missing from list: %s", getResponse.Body)
	}
}

func TestPublicProjectionContainsOnlySafeFields(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	projectionStore := &stubProjectionStore{view: projection.View{
		UserID: "manager-1", Timezone: "Asia/Tokyo", GeneratedAt: now,
		Segments: []projection.Segment{{
			StartAt: now, EndAt: now.Add(time.Hour),
			Availability: policy.Limited, Interruptibility: policy.UrgentOnly,
			Requestability: policy.RequestLater, Reschedulability: policy.RescheduleLow,
			ExpectedResponseBucket: "unknown",
		}},
	}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, projectionStore, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people/manager-1/projection?timezone=Asia%2FTokyo", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	encoded := response.Body.String()
	for _, expected := range []string{"segments", "availability", "interruptibility", "requestability", "reschedulability"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("public projection missing %q: %s", expected, encoded)
		}
	}
	for _, forbidden := range []string{"title", "description", "location", "attendees", "organizer", "providerEventId", "calendarName"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, encoded)
		}
	}
}

func TestPeopleResponseOmitsEmailAndCalendarDetails(t *testing.T) {
	t.Parallel()
	organizations := &stubOrganizationStore{people: []organization.Person{{
		ID: "manager-1", DisplayName: "山田 太郎", Timezone: "Asia/Tokyo", Role: organization.Manager,
	}}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, organizations, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people?organizationId=org-1", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	encoded := response.Body.String()
	for _, expected := range []string{"manager-1", "山田 太郎", "MANAGER"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("people response missing %q: %s", expected, encoded)
		}
	}
	for _, forbidden := range []string{"email", "title", "location", "attendees", "calendar"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, encoded)
		}
	}
}

func TestPeopleRequiresOrganizationID(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestCoordinationRequestCreate(t *testing.T) {
	t.Parallel()
	requests := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, requests, "", testLogger())
	body, err := json.Marshal(map[string]any{
		"targetUserId":    "demo-manager",
		"type":            "review",
		"title":           "API design review",
		"durationMinutes": 15,
		"deadlineAt":      time.Now().UTC().Add(4 * time.Hour),
		"syncPreference":  "either",
		"priority":        "normal",
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "demo-member")
	request.Header.Set("X-Organization-ID", "demo-org")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body)
	}
	if requests.value.Status != coordinationrequest.Suggested {
		t.Fatalf("new request status is %q", requests.value.Status)
	}
	if len(requests.value.Options) != 1 || requests.value.Options[0].Type != coordinationrequest.OptionAsync {
		t.Fatalf("expected async fallback option, got %#v", requests.value.Options)
	}
	if requests.value.RequesterUserID != "demo-member" {
		t.Fatalf("requester identity was not persisted")
	}
	for _, forbidden := range []string{"privateEvent", "attendees", "location", "calendar", "score"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, response.Body)
		}
	}
}

func TestCoordinationRequestRequiresIdentity(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestNotificationsListAndReadUseAuthenticatedUser(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	store := &stubNotificationStore{
		values: []notification.Notification{{
			ID: "notification-1", UserID: "manager-1", Type: notification.RequestReceived,
			RequestID: "request-1", Message: "新しい調整依頼が届きました。", CreatedAt: now,
		}},
		readResult: true,
	}
	handler := NewWithNotifications(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, store, "", testLogger())
	list := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	list.Header.Set("X-Demo-User-ID", "manager-1")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || store.listUser != "manager-1" {
		t.Fatalf("notification list failed: %d user=%q", listResponse.Code, store.listUser)
	}
	for _, forbidden := range []string{"privateEvent", "attendees", "location", "calendar"} {
		if strings.Contains(listResponse.Body.String(), forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, listResponse.Body)
		}
	}
	read := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/notification-1/read", nil)
	read.Header.Set("X-Demo-User-ID", "manager-1")
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusNoContent || store.readID != "notification-1" || store.readUser != "manager-1" {
		t.Fatalf("notification read failed: %d id=%q user=%q", readResponse.Code, store.readID, store.readUser)
	}
}

func TestRequestCreateEmitsPrivacySafeNotification(t *testing.T) {
	t.Parallel()
	notifications := &stubNotificationStore{}
	handler := NewWithNotifications(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, notifications, "", testLogger())
	body, _ := json.Marshal(map[string]any{
		"targetUserId": "manager-1", "type": "review", "title": "Secret acquisition review",
		"durationMinutes": 15, "deadlineAt": time.Now().UTC().Add(4 * time.Hour),
		"syncPreference": "either", "priority": "normal",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || len(notifications.values) != 1 {
		t.Fatalf("request notification missing: status=%d values=%#v", response.Code, notifications.values)
	}
	created := notifications.values[0]
	if created.UserID != "manager-1" || created.Type != notification.RequestReceived {
		t.Fatalf("unexpected notification: %#v", created)
	}
	if strings.Contains(created.Message, "Secret acquisition review") {
		t.Fatalf("request title leaked into notification: %q", created.Message)
	}
}

func TestAuditLogsUseOrganizationBoundaryAndSafeFields(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	audits := &stubAuditStore{values: []audit.Event{{
		ID: "audit-1", OrganizationID: "org-1", ActorUserID: "manager-1",
		Action: audit.RequestAccepted, ResourceType: "request", ResourceID: "request-1", CreatedAt: now,
	}}}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, &stubNotificationStore{}, audits, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || audits.listOrganization != "org-1" {
		t.Fatalf("audit list failed: %d organization=%q", response.Code, audits.listOrganization)
	}
	encoded := response.Body.String()
	for _, expected := range []string{"auditLogs", "request_accepted", "manager-1", "request-1"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("audit response missing %q: %s", expected, encoded)
		}
	}
	for _, forbidden := range []string{"title", "privateEvent", "attendees", "location", "calendar"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, encoded)
		}
	}
}

func TestRequestCreateEmitsPrivacySafeAuditEvent(t *testing.T) {
	t.Parallel()
	audits := &stubAuditStore{}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, &stubNotificationStore{}, audits, "", testLogger())
	body, _ := json.Marshal(map[string]any{
		"targetUserId": "manager-1", "type": "review", "title": "Confidential board review",
		"durationMinutes": 15, "deadlineAt": time.Now().UTC().Add(4 * time.Hour),
		"syncPreference": "either", "priority": "normal",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || len(audits.values) != 1 {
		t.Fatalf("audit event missing: status=%d values=%#v", response.Code, audits.values)
	}
	event := audits.values[0]
	if event.ActorUserID != "member-1" || event.Action != audit.RequestCreated || event.ResourceType != "request" {
		t.Fatalf("unexpected audit event: %#v", event)
	}
}

func TestCoordinationRequestInboxUsesAuthenticatedTargetAndSafeFields(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	startAt := now.Add(time.Hour)
	endAt := startAt.Add(15 * time.Minute)
	requests := &stubRequestStore{values: []coordinationrequest.CoordinationRequest{{
		ID: "request-1", OrganizationID: "org-1", RequesterUserID: "member-1", TargetUserID: "manager-1",
		Type: coordinationrequest.Review, Title: "API review", DurationMinutes: 15,
		DeadlineAt: now.Add(4 * time.Hour), SyncPreference: coordinationrequest.Either,
		Priority: coordinationrequest.PriorityNormal, Status: coordinationrequest.Suggested,
		Options: []coordinationrequest.Option{{
			ID: "option-1", RequestID: "request-1", Type: coordinationrequest.OptionMeeting,
			StartAt: &startAt, EndAt: &endAt, CreatedAt: now,
		}}, CreatedAt: now, UpdatedAt: now,
	}}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, requests, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	if requests.target != "manager-1" {
		t.Fatalf("authenticated target not used: %q", requests.target)
	}
	encoded := response.Body.String()
	for _, expected := range []string{"requests", "API review", "option-1", "meeting"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("inbox response missing %q: %s", expected, encoded)
		}
	}
	for _, forbidden := range []string{"privateEvent", "attendees", "location", "calendar", "score"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("private field %q leaked: %s", forbidden, encoded)
		}
	}
}

func TestCoordinationRequestInboxRequiresIdentity(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestSharingPolicyRejectsIDOR(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/sharing-policy", nil)
	request.Header.Set("X-Demo-User-ID", "attacker-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for policy IDOR, got %d", response.Code)
	}
}

func TestPeopleRejectsOrganizationSpoofing(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people?organizationId=org-1", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "other-org")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for organization spoofing, got %d", response.Code)
	}
}

func TestAuditLogsRejectNonMember(t *testing.T) {
	t.Parallel()
	organizations := &stubOrganizationStore{members: map[string]bool{"org-1:member-1": false}}
	audits := &stubAuditStore{}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, organizations, &stubRequestStore{}, &stubNotificationStore{}, audits, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || audits.listOrganization != "" {
		t.Fatalf("non-member reached audit store: status=%d organization=%q", response.Code, audits.listOrganization)
	}
}

func TestRequestCreateRejectsTargetOutsideOrganization(t *testing.T) {
	t.Parallel()
	organizations := &stubOrganizationStore{members: map[string]bool{
		"org-1:member-1":  true,
		"org-1:manager-2": false,
	}}
	requests := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, organizations, requests, "", testLogger())
	body, _ := json.Marshal(map[string]any{
		"targetUserId": "manager-2", "type": "review", "title": "Cross-org probe",
		"durationMinutes": 15, "deadlineAt": time.Now().UTC().Add(4 * time.Hour),
		"syncPreference": "either", "priority": "normal",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || requests.value.ID != "" {
		t.Fatalf("cross-org request persisted: status=%d value=%#v", response.Code, requests.value)
	}
}

func TestProjectionRejectsTargetOutsideOrganization(t *testing.T) {
	t.Parallel()
	organizations := &stubOrganizationStore{members: map[string]bool{
		"org-1:member-1":  true,
		"org-1:manager-2": false,
	}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, organizations, &stubRequestStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people/manager-2/projection", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	request.Header.Set("X-Organization-ID", "org-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-org projection, got %d", response.Code)
	}
}

func TestUserDataExportReturnsOnlySelfServiceData(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	requests := &stubRequestStore{values: []coordinationrequest.CoordinationRequest{{
		ID: "request-1", RequesterUserID: "manager-1", TargetUserID: "member-1", Title: "Review",
	}}}
	projections := &stubProjectionStore{values: []projection.ScheduleProjection{{
		ID: "projection-1", UserID: "manager-1", StartAt: now, EndAt: now.Add(time.Hour),
	}}}
	policies := &stubPolicyStore{value: policy.SharingPolicy{ID: "policy-1", UserID: "manager-1"}}
	handler := New(stubDatabase{}, policies, projections, &stubOrganizationStore{}, requests, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/export", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected export success, got %d: %s", response.Code, response.Body.String())
	}
	if requests.target != "manager-1" {
		t.Fatalf("export queried wrong user: %q", requests.target)
	}
	if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, "negotiable-calendar-manager-1.json") {
		t.Fatalf("unexpected content disposition: %q", disposition)
	}
	body := response.Body.String()
	for _, expected := range []string{`"userId":"manager-1"`, `"requests"`, `"policy"`, `"projections"`, `"generatedAt"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("export omitted %s: %s", expected, body)
		}
	}
	if strings.Contains(body, "privateEvents") {
		t.Fatalf("export leaked private event structure: %s", body)
	}
}

func TestUserDataExportRejectsIDOR(t *testing.T) {
	t.Parallel()
	requests := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, requests, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/export", nil)
	request.Header.Set("X-Demo-User-ID", "attacker-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || requests.target != "" {
		t.Fatalf("IDOR reached export store: status=%d user=%q", response.Code, requests.target)
	}
}

func TestRequestListSupportsRequesterSentScope(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{values: []coordinationrequest.CoordinationRequest{{ID: "request-1", RequesterUserID: "member-1"}}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests?scope=sent", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || store.target != "member-1" {
		t.Fatalf("sent scope failed: status=%d requester=%q", response.Code, store.target)
	}
	if !strings.Contains(response.Body.String(), `"id":"request-1"`) {
		t.Fatalf("sent request missing: %s", response.Body.String())
	}
}

func TestRequestDetailIsLoadedForParticipant(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{value: coordinationrequest.CoordinationRequest{
		ID: "request-1", RequesterUserID: "member-1", TargetUserID: "manager-1",
	}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/requests/request-1", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || store.getUser != "manager-1" {
		t.Fatalf("participant detail failed: status=%d user=%q", response.Code, store.getUser)
	}
}

func TestRequesterCanCancelActiveRequest(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{value: coordinationrequest.CoordinationRequest{
		ID: "request-1", RequesterUserID: "member-1", TargetUserID: "manager-1", Status: coordinationrequest.Suggested,
	}}
	notifications := &stubNotificationStore{}
	audits := &stubAuditStore{}
	handler := NewWithStores(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, notifications, audits, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/cancel", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || store.cancelledID != "request-1" || store.cancelledUser != "member-1" {
		t.Fatalf("request was not cancelled: status=%d id=%q user=%q", response.Code, store.cancelledID, store.cancelledUser)
	}
	if len(notifications.values) != 1 || notifications.values[0].UserID != "manager-1" || notifications.values[0].Type != notification.RequestCancelled {
		t.Fatalf("target cancellation notification missing: %#v", notifications.values)
	}
	if len(audits.values) != 1 || audits.values[0].Action != audit.RequestCancelled || audits.values[0].ResourceID != "request-1" {
		t.Fatalf("cancellation audit missing: %#v", audits.values)
	}
}

func TestRequestTargetCannotCancel(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{value: coordinationrequest.CoordinationRequest{
		ID: "request-1", RequesterUserID: "member-1", TargetUserID: "manager-1", Status: coordinationrequest.Suggested,
	}}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/cancel", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || store.cancelledID != "" {
		t.Fatalf("target reached cancellation: status=%d id=%q", response.Code, store.cancelledID)
	}
}

func TestTerminalRequestCannotBeCancelled(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{
		value: coordinationrequest.CoordinationRequest{
			ID: "request-1", RequesterUserID: "member-1", TargetUserID: "manager-1", Status: coordinationrequest.Accepted,
		},
		cancelErr: coordinationrequest.ErrNotFound,
	}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/cancel", nil)
	request.Header.Set("X-Demo-User-ID", "member-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected terminal conflict, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCoordinationRequestAcceptsSelectedOptionAsTarget(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/accept", strings.NewReader(`{"optionId":"option-1"}`))
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	if store.respondID != "request-1" || store.respondTarget != "manager-1" || store.respondStatus != coordinationrequest.Accepted || store.respondOption != "option-1" {
		t.Fatalf("unexpected accept call: %#v", store)
	}
}

func TestCoordinationRequestDeclinesAsTarget(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/decline", nil)
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.respondStatus != coordinationrequest.Declined {
		t.Fatalf("decline failed: status=%d store=%#v", response.Code, store)
	}
}

func TestCoordinationRequestSuggestsMeetingAsTarget(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	startAt := time.Now().UTC().Add(2 * time.Hour)
	body, _ := json.Marshal(map[string]any{"startAt": startAt, "endAt": startAt.Add(30 * time.Minute)})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/suggest", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body)
	}
	if store.respondID != "request-1" || store.respondTarget != "manager-1" || store.suggestedOption.Type != coordinationrequest.OptionMeeting {
		t.Fatalf("unexpected suggest call: %#v", store)
	}
}

func TestCoordinationRequestRejectsPastSuggestion(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	startAt := time.Now().UTC().Add(-2 * time.Hour)
	body, _ := json.Marshal(map[string]any{"startAt": startAt, "endAt": startAt.Add(30 * time.Minute)})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/suggest", bytes.NewReader(body))
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || store.suggestedOption.ID != "" {
		t.Fatalf("past suggestion was accepted: %d %#v", response.Code, store.suggestedOption)
	}
}

func TestCoordinationRequestDelegatesAsTarget(t *testing.T) {
	t.Parallel()
	store := &stubRequestStore{}
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requests/request-1/delegate", strings.NewReader(`{"delegateUserId":"member-2"}`))
	request.Header.Set("X-Demo-User-ID", "manager-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}
	if store.respondID != "request-1" || store.respondTarget != "manager-1" || store.delegatedOption.DelegateUserID != "member-2" {
		t.Fatalf("unexpected delegate call: %#v", store)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
