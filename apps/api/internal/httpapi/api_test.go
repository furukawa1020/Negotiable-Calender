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

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
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
	view projection.View
	err  error
}

type stubOrganizationStore struct {
	people []organization.Person
	err    error
}

func (store *stubOrganizationStore) ListPeople(context.Context, string) ([]organization.Person, error) {
	return store.people, store.err
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
	New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger()).ServeHTTP(response, request)

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
	New(stubDatabase{err: errors.New("database unavailable")}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger()).ServeHTTP(response, request)

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

	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, "https://calendar.example", testLogger())
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
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, "https://calendar.example", testLogger())
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
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger())
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
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("expected PUT 200, got %d: %s", putResponse.Code, putResponse.Body)
	}
	if store.value.UserID != "manager-1" {
		t.Fatalf("path user was not persisted: %q", store.value.UserID)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/users/manager-1/sharing-policy", nil)
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
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger())
	body := `{"default":{"availability":"secret_meeting"},"workingHours":[],"rules":[]}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/manager-1/sharing-policy", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", response.Code, response.Body)
	}
}

func TestSharingPolicyGetReturnsNotFound(t *testing.T) {
	t.Parallel()
	handler := New(stubDatabase{}, &stubPolicyStore{err: policy.ErrNotFound}, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/missing/sharing-policy", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}

func TestManualOverrideCreateThenList(t *testing.T) {
	t.Parallel()
	store := &stubPolicyStore{}
	handler := New(stubDatabase{}, store, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger())
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
	handler := New(stubDatabase{}, &stubPolicyStore{}, projectionStore, &stubOrganizationStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people/manager-1/projection?timezone=Asia%2FTokyo", nil)
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
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, organizations, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people?organizationId=org-1", nil)
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
	handler := New(stubDatabase{}, &stubPolicyStore{}, &stubProjectionStore{}, &stubOrganizationStore{}, "", testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/people", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
