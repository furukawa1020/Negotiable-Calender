package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func TestAccountDeletionMaintainsPostgresReferentialIntegrity(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := fmt.Sprintf("account_delete_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer database.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	migrations := []func(context.Context, *sql.DB) error{
		policy.EnsureSchema,
		organization.EnsureSchema,
		auth.EnsureSchema,
		calendarintegration.EnsureSchema,
		calendarintegration.EnsureBackgroundSchema,
		projection.EnsureSchema,
		coordinationrequest.EnsureSchema,
		notification.EnsureSchema,
		audit.EnsureSchema,
		organization.EnsureInvitationSchema,
	}
	for _, migrate := range migrations {
		if err := migrate(ctx, database); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	exec := func(query string, arguments ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, arguments...); err != nil {
			t.Fatalf("seed deletion fixture: %v", err)
		}
	}
	exec(`INSERT INTO organizations(id,name,created_at,updated_at) VALUES
('shared-org','Shared',$1,$1),('solo-org','Solo',$1,$1)`, now)
	exec(`INSERT INTO users(id,email,display_name,timezone,created_at,updated_at) VALUES
('owner-1','owner@example.com','Owner','Asia/Tokyo',$1,$1),
('owner-2','other@example.com','Other','Asia/Tokyo',$1,$1),
('solo-1','solo@example.com','Solo','Asia/Tokyo',$1,$1)`, now)
	exec(`INSERT INTO memberships(id,organization_id,user_id,role,created_at) VALUES
('membership-1','shared-org','owner-1','OWNER',$1),
('membership-2','shared-org','owner-2','MEMBER',$1),
('membership-3','solo-org','solo-1','OWNER',$1)`, now)
	exec(`INSERT INTO auth_identities(provider,subject,user_id,email,created_at,updated_at)
VALUES('google','subject-1','owner-1','owner@example.com',$1,$1)`, now)
	exec(`INSERT INTO auth_sessions(token_hash,user_id,organization_id,expires_at,created_at)
VALUES($1,'owner-1','shared-org',$2,$3)`, []byte("session-hash"), now.Add(time.Hour), now)
	exec(`INSERT INTO calendar_oauth_flows(id,user_id,state_hash,code_verifier,expires_at,created_at)
VALUES('flow-1','owner-1',$1,'verifier',$2,$3)`, []byte("state-hash"), now.Add(time.Hour), now)
	exec(`INSERT INTO calendar_connections(user_id,refresh_token_cipher,granted_scopes,connected_at)
VALUES('owner-1',$1,$2,$3)`, []byte("encrypted-token"), calendarintegration.CalendarReadonlyScope, now)
	exec(`INSERT INTO private_events(user_id,provider_event_id,calendar_id,start_at,end_at,busy_status,visibility,created_at,updated_at)
VALUES('owner-1','event-1','primary',$1,$2,'busy','private',$3,$3)`, now, now.Add(time.Hour), now)
	exec(`INSERT INTO sharing_policies(id,user_id,default_availability,default_interruptibility,default_requestability,default_reschedulability,working_hours_json,created_at,updated_at)
VALUES('policy-1','owner-1','available','normal','open','medium','[]',$1,$1)`, now)
	exec(`INSERT INTO manual_overrides(id,user_id,start_at,end_at,availability,interruptibility,requestability,reschedulability,expires_at,created_at)
VALUES('override-1','owner-1',$1,$2,'limited','urgent_only','later','low',$2,$1)`, now, now.Add(time.Hour))
	exec(`INSERT INTO schedule_projections(id,user_id,start_at,end_at,availability,interruptibility,requestability,reschedulability,expected_response_bucket,generated_at,expires_at)
VALUES('projection-1','owner-1',$1,$2,'limited','urgent_only','later','low','unknown',$1,$2)`, now, now.Add(time.Hour))
	exec(`INSERT INTO coordination_requests(id,organization_id,requester_user_id,target_user_id,type,title,duration_minutes,deadline_at,sync_preference,priority,status,created_at,updated_at)
VALUES('request-1','shared-org','owner-1','owner-2','review','Private request',15,$1,'either','normal','suggested',$2,$2)`, now.Add(time.Hour), now)
	exec(`INSERT INTO coordination_request_options(id,request_id,type,created_at)
VALUES('option-1','request-1','async',$1)`, now)
	exec(`INSERT INTO notifications(id,user_id,type,request_id,message,created_at)
VALUES('notification-1','owner-2','request_received','request-1','generic',$1)`, now)
	exec(`INSERT INTO audit_logs(id,organization_id,actor_user_id,action,resource_type,resource_id,created_at)
VALUES('audit-1','shared-org','owner-2','request_changed','request','request-1',$1)`, now)
	exec(`INSERT INTO organization_invitations(id,organization_id,invited_by,role,token_hash,expires_at,accepted_by,created_at)
VALUES('invitation-1','shared-org','owner-2','MEMBER',$1,$2,'owner-1',$3)`, []byte("invite-hash"), now.Add(time.Hour), now)

	store := auth.NewPostgresStore(database)
	if err := store.DeleteAccount(ctx, "owner-1"); !errors.Is(err, auth.ErrLastOrganizationOwner) {
		t.Fatalf("last owner was not protected: %v", err)
	}
	assertCount(t, ctx, database, "users", "id = 'owner-1'", 1)
	assertCount(t, ctx, database, "calendar_connections", "user_id = 'owner-1'", 1)

	exec(`UPDATE memberships SET role='OWNER' WHERE user_id='owner-2'`)
	if err := store.DeleteAccount(ctx, "owner-1"); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	for _, check := range []struct{ table, predicate string }{
		{"users", "id = 'owner-1'"},
		{"memberships", "user_id = 'owner-1'"},
		{"auth_identities", "user_id = 'owner-1'"},
		{"auth_sessions", "user_id = 'owner-1'"},
		{"calendar_oauth_flows", "user_id = 'owner-1'"},
		{"calendar_connections", "user_id = 'owner-1'"},
		{"private_events", "user_id = 'owner-1'"},
		{"sharing_policies", "user_id = 'owner-1'"},
		{"manual_overrides", "user_id = 'owner-1'"},
		{"schedule_projections", "user_id = 'owner-1'"},
		{"coordination_requests", "id = 'request-1'"},
		{"notifications", "request_id = 'request-1'"},
		{"audit_logs", "resource_id = 'request-1'"},
		{"organization_invitations", "id = 'invitation-1'"},
	} {
		assertCount(t, ctx, database, check.table, check.predicate, 0)
	}
	assertCount(t, ctx, database, "users", "id = 'owner-2'", 1)
	assertCount(t, ctx, database, "organizations", "id = 'shared-org'", 1)

	if err := store.DeleteAccount(ctx, "solo-1"); err != nil {
		t.Fatalf("delete solo account: %v", err)
	}
	assertCount(t, ctx, database, "organizations", "id = 'solo-org'", 0)
}

func assertCount(t *testing.T, ctx context.Context, database *sql.DB, table, predicate string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("%s count = %d, want %d", table, count, expected)
	}
}
