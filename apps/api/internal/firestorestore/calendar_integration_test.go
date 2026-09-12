package firestorestore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

type calendarRoundTrip func(*http.Request) (*http.Response, error)
func (f calendarRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCalendarOAuthSyncPublicationAndDisconnect(t *testing.T) {
	b, ctx := emulatorBackend(t)
	now := time.Now().UTC().Truncate(time.Second)
	putDocument(t, ctx, b.Client.Collection("users").Doc("alice"), userRecord{ID: "alice", Timezone: "Asia/Tokyo"})
	cipher, err := calendarintegration.NewTokenCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil { t.Fatal(err) }
	challenge := ""
	eventCalls := 0
	cursors := []string{}
	client := &http.Client{Transport: calendarRoundTrip(func(r *http.Request) (*http.Response, error) {
		var body any
		switch r.URL.Host + r.URL.Path {
		case "oauth2.googleapis.com/token":
			if err := r.ParseForm(); err != nil { return nil, err }
			if r.Form.Get("grant_type") == "authorization_code" {
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "synthetic-code" {
					return nil, errors.New("invalid PKCE exchange")
				}
			} else if r.Form.Get("refresh_token") != "synthetic-refresh" {
				return nil, errors.New("invalid decrypted refresh token")
			}
			body = map[string]any{"access_token":"synthetic-access", "refresh_token":"synthetic-refresh", "expires_in":3600, "scope":calendarintegration.CalendarReadonlyScope}
		case "www.googleapis.com/calendar/v3/calendars/primary/events":
			if r.Header.Get("Authorization") != "Bearer synthetic-access" { return nil, errors.New("missing access token") }
			if strings.Contains(r.URL.Query().Get("fields"), "summary") { return nil, errors.New("sync requested private event titles") }
			cursor := r.URL.Query().Get("syncToken")
			cursors = append(cursors, cursor)
			eventCalls++
			id := "event-1"
			items := []any{}
			if cursor == "cursor-1" {
				id = "event-2"
				items = append(items, map[string]any{"id":"event-1", "status":"cancelled"})
			}
			items = append(items, map[string]any{"id":id, "start":map[string]string{"dateTime":now.Add(time.Hour).Format(time.RFC3339)}, "end":map[string]string{"dateTime":now.Add(2*time.Hour).Format(time.RFC3339)}, "summary":"MUST_NOT_PERSIST", "description":"PRIVATE_DESCRIPTION"})
			body = map[string]any{"items":items, "nextSyncToken":fmt.Sprintf("cursor-%d",eventCalls)}
		default:
			return nil, fmt.Errorf("unexpected external request: %s", r.URL.Host)
		}
		data, err := json.Marshal(body)
		if err != nil { return nil, err }
		return &http.Response{StatusCode:200, Header:make(http.Header), Body:io.NopCloser(strings.NewReader(string(data))), Request:r}, nil
	})}
	provider := calendarintegration.NewGoogleProvider(calendarintegration.GoogleConfig{ClientID:"synthetic-client", ClientSecret:"synthetic-secret", RedirectURL:"https://app.example/api/v1/calendar/google/callback"}, client)
	handler := calendarintegration.NewHandler(http.NotFoundHandler(), b.Calendar(), provider, cipher, projection.NewRebuilder(b.Calendar(), b.Policy()), calendarintegration.HandlerConfig{WebOrigin:"https://app.example", SyncPast:time.Hour, SyncFuture:24*time.Hour}, slog.New(slog.NewTextHandler(io.Discard,nil)))
	call := func(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method,path,nil).WithContext(ctx)
		r.Header.Set(auth.AuthenticatedUserHeader,"alice")
		for _, c := range cookies { r.AddCookie(c) }
		w := httptest.NewRecorder()
		handler.ServeHTTP(w,r)
		return w
	}
	connect := func() {
		t.Helper()
		w := call(http.MethodGet,"/api/v1/calendar/google/connect")
		if w.Code != http.StatusFound { t.Fatalf("connect: %d",w.Code) }
		target, err := url.Parse(w.Header().Get("Location"))
		if err != nil { t.Fatal(err) }
		challenge = target.Query().Get("code_challenge")
		if challenge == "" || target.Query().Get("scope") != calendarintegration.CalendarReadonlyScope { t.Fatal("invalid consent request") }
		callback := "/api/v1/calendar/google/callback?state="+url.QueryEscape(target.Query().Get("state"))+"&code=synthetic-code"
		cookies := w.Result().Cookies()
		if w = call(http.MethodGet,callback,cookies...); w.Code != http.StatusFound { t.Fatalf("callback: %d %s",w.Code,w.Body.String()) }
		if w = call(http.MethodGet,callback,cookies...); w.Code != http.StatusBadRequest { t.Fatal("OAuth flow reused") }
	}
	sync := func(want int) {
		t.Helper()
		w := call(http.MethodPost,"/api/v1/calendar/sync")
		if w.Code != want { t.Fatalf("sync: %d %s",w.Code,w.Body.String()) }
	}
	assertHidden := func() {
		t.Helper()
		all, err := b.Projection().ListForUser(ctx,"alice")
		if err != nil || len(all) != 0 { t.Fatalf("public list not hidden: %d %v",len(all),err) }
		view, err := b.Projection().GetView(ctx,"alice","Asia/Tokyo",now,now.Add(24*time.Hour))
		if err != nil || len(view.Segments) != 0 { t.Fatalf("public view not hidden: %v",err) }
	}
	connect()
	connection, err := b.Calendar().GetConnection(ctx,"alice")
	if err != nil || len(connection.RefreshTokenCipher)==0 || strings.Contains(string(connection.RefreshTokenCipher),"synthetic-refresh") { t.Fatal("grant not encrypted") }
	sync(http.StatusOK)
	sync(http.StatusOK)
	if len(cursors)!=2 || cursors[0]!="" || cursors[1]!="cursor-1" { t.Fatalf("incremental cursor chain: %v",cursors) }
	events, err := b.Calendar().ListPrivateEvents(ctx,"alice",now,now.Add(24*time.Hour))
	if err != nil || len(events)!=1 || events[0].ProviderEventID!="event-2" { t.Fatalf("incremental replacement: %v %v",events,err) }
	docs, err := b.Client.Collection("users").Doc("alice").Collection("privateEvents").Documents(ctx).GetAll()
	if err != nil { t.Fatal(err) }
	for _, d := range docs {
		raw,_ := json.Marshal(d.Data())
		if strings.Contains(string(raw),"MUST_NOT_PERSIST") || strings.Contains(string(raw),"PRIVATE_DESCRIPTION") { t.Fatal("private details persisted by sync") }
	}
	published, err := b.Projection().ListForUser(ctx,"alice")
	if err != nil || len(published)==0 { t.Fatalf("sync did not publish: %v",err) }
	connection, err = b.Calendar().GetConnection(ctx,"alice")
	if err != nil || connection.SyncToken!="cursor-2" || connection.LastSyncedAt==nil { t.Fatal("sync completion missing") }
	putDocument(t,ctx,b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc("other"),published[0])
	for i:=0;i<2;i++ {
		w := call(http.MethodDelete,"/api/v1/calendar/connection")
		if w.Code!=http.StatusNoContent { t.Fatalf("disconnect: %d",w.Code) }
	}
	assertHidden()
	if _,err:=b.Calendar().GetConnection(ctx,"alice"); !errors.Is(err,calendarintegration.ErrNotFound) { t.Fatalf("connection remains: %v",err) }
	events,err=b.Calendar().ListPrivateEvents(ctx,"alice",now,now.Add(24*time.Hour))
	if err!=nil || len(events)!=0 { t.Fatal("private events remain") }
	if _,err:=b.Client.Collection("users").Doc("bob").Collection("scheduleProjections").Doc("other").Get(ctx);err!=nil { t.Fatal("other user changed") }
	// Failed completion must not clear the marker when the connection is gone.
	if err:=b.Calendar().MarkSyncSuccess(ctx,"alice","stale",now,now.Add(time.Hour));err==nil { t.Fatal("missing connection completed") }
	assertHidden()
	connect()
	assertHidden()
	// A failed real projection rebuild after reconnect must keep publication blocked.
	putDocument(t,ctx,b.Client.Collection("sharingPolicies").Doc("alice"),map[string]any{"Default":"invalid fixture"})
	sync(http.StatusInternalServerError)
	assertHidden()
	if _,err:=b.Client.Collection("sharingPolicies").Doc("alice").Delete(ctx);err!=nil { t.Fatal(err) }
	sync(http.StatusOK)
	published,err=b.Projection().ListForUser(ctx,"alice")
	if err!=nil || len(published)==0 { t.Fatalf("successful resync did not reopen publication: %v",err) }
}

func TestDisconnectedProjectionMarkerHidesInterruptedCleanup(t *testing.T) {
	b,ctx:=emulatorBackend(t)
	// Model a process exit after the marker commit and before data cleanup.
	putDocument(t,ctx,b.projectionBlock("alice"),map[string]any{"BlockedAt":time.Now().UTC()})
	putDocument(t,ctx,b.Client.Collection("users").Doc("alice").Collection("scheduleProjections").Doc("old"),map[string]any{"ID":123})
	values,err:=b.Projection().ListForUser(ctx,"alice")
	if err!=nil || values==nil || len(values)!=0 { t.Fatalf("interrupted cleanup exposed old data: %v",err) }
	if err:=b.Calendar().DeleteConnection(ctx,"alice");err!=nil { t.Fatal(err) }
}
