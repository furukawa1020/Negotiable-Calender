package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandsRejectInvalidWholeBodyBeforeSideEffects(t *testing.T) {
	for _, route := range []struct{ path, body string }{
		{"/api/v1/requests", `{"targetUserId":"peer","type":"meeting","title":"test","durationMinutes":30,"deadlineAt":"2099-01-01T00:00:00Z","syncPreference":"sync","priority":"normal"}`},
		{"/api/v1/requests/r/accept", `{"optionId":"o"}`},
		{"/api/v1/requests/r/suggest", `{"startAt":"2098-01-01T00:00:00Z","endAt":"2098-01-01T00:30:00Z"}`},
		{"/api/v1/requests/r/delegate", `{"delegateUserId":"peer"}`},
		{"/api/v1/requests/r/async", `{"message":"answer"}`},
		{"/api/v1/users/actor/manual-overrides", `{"startAt":"2098-01-01T00:00:00Z","endAt":"2098-01-01T00:30:00Z","expiresAt":"2098-01-02T00:30:00Z","state":{"availability":"available","interruptibility":"open","requestability":"open","reschedulability":"high"}}`},
	} {
		for _, body := range []string{route.body + `{}`, route.body + `null`, route.body + `garbage`, `null`, `[]`, `{"unknown":true}`, route.body + strings.Repeat(" ", 1<<20)} {
			t.Run(route.path+"/"+body[:min(len(body), 12)], func(t *testing.T) {
				store, policies := &stubRequestStore{}, &stubPolicyStore{}
				handler := New(stubDatabase{}, policies, &stubProjectionStore{}, &stubOrganizationStore{}, store, "", testLogger())
				req := httptest.NewRequest("POST", route.path, strings.NewReader(body))
				req.Header.Set("X-Demo-User-ID", "actor")
				req.Header.Set("X-Organization-ID", "org")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != 400 {
					t.Fatalf("status=%d body=%s", response.Code, response.Body)
				}
				if store.value.ID != "" || store.getUser != "" || store.respondID != "" || store.suggestedOption.ID != "" || store.delegatedOption.ID != "" || store.asyncMessage != "" || len(policies.overrides) != 0 {
					t.Fatal("invalid command had side effects")
				}
			})
		}
	}
}

func TestCommandAllowsTrailingWhitespace(t *testing.T) {
	var value struct {
		Name string `json:"name"`
	}
	err := decodeCommand(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("{\"name\":\"ok\"} \r\n\t")), &value, 128)
	if err != nil || value.Name != "ok" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}
