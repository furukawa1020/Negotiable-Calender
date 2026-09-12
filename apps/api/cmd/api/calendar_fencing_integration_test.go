package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
)

func TestPostgresCalendarSyncFencing(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
	db,err:=sql.Open("pgx",dsn);if err!=nil{t.Fatal(err)}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
	schema:=fmt.Sprintf("calendar_fence_%d",time.Now().UnixNano())
	exec:=func(query string,args ...any){t.Helper();if _,err:=db.ExecContext(ctx,query,args...);err!=nil{t.Fatal(err)}}
	exec("CREATE SCHEMA "+schema)
	defer db.ExecContext(context.Background(),"DROP SCHEMA "+schema+" CASCADE")
	exec("SET search_path TO "+schema)
	for _,migrate:=range []func(context.Context,*sql.DB)error{policy.EnsureSchema,organization.EnsureSchema,auth.EnsureSchema,calendarintegration.EnsureSchema,calendarintegration.EnsureBackgroundSchema,projection.EnsureSchema}{
		if err:=migrate(ctx,db);err!=nil{t.Fatal(err)}
	}
	now:=time.Now().UTC()
	exec("INSERT INTO users(id,email,display_name,timezone,created_at,updated_at) VALUES('alice','alice@example.test','Alice','Asia/Tokyo',$1,$1)",now)
	store:=calendarintegration.NewPostgresStore(db)
	connection:=calendarintegration.Connection{UserID:"alice",ConnectedAt:now,RefreshTokenCipher:[]byte("synthetic"),GrantedScopes:[]string{calendarintegration.CalendarReadonlyScope}}
	for _,scenario:=range []string{"reconnect","disconnect","expired"}{
		t.Run(scenario,func(t *testing.T){
			if err:=store.SaveConnection(ctx,connection);err!=nil{t.Fatal(err)}
			owner,err:=store.AcquireSync(ctx,"alice",time.Now().UTC(),time.Minute);if err!=nil{t.Fatal(err)}
			if _,err:=store.AcquireSync(ctx,"alice",time.Now().UTC(),time.Minute);!errors.Is(err,calendarintegration.ErrSyncBusy){t.Fatalf("duplicate sync: %v",err)}
			stale:=calendarintegration.WithSyncLease(ctx,calendarintegration.SyncLease{UserID:"alice",ID:owner.SyncLeaseID})
			switch scenario{
			case "reconnect":if err:=store.SaveConnection(ctx,connection);err!=nil{t.Fatal(err)}
			case "disconnect":if err:=store.DeleteConnection(ctx,"alice");err!=nil{t.Fatal(err)}
			case "expired":
				exec("UPDATE calendar_connections SET sync_lease_until=$1 WHERE user_id='alice'",now.Add(-time.Minute))
				next,err:=store.AcquireSync(ctx,"alice",time.Now().UTC(),time.Minute);if err!=nil||next.SyncLeaseID==owner.SyncLeaseID{t.Fatalf("lease not replaced: %v",err)}
			}
			span:=calendarintegration.BusySpan{ProviderEventID:"old",CalendarID:"primary",StartAt:now,EndAt:now.Add(time.Hour),Busy:true}
			for name,write:=range map[string]func()error{
				"events":func()error{return store.ApplyChanges(stale,"alice",calendarintegration.ChangeSet{Full:true,Upserts:[]calendarintegration.BusySpan{span}},now,now.Add(time.Hour),now)},
				"legacy-events":func()error{return store.ReplaceBusySpans(stale,"alice",[]calendarintegration.BusySpan{span},now,now.Add(time.Hour),now)},
				"projection":func()error{return projection.NewPostgresStore(db).Replace(stale,"alice",now,now.Add(time.Hour),nil)},
				"success":func()error{return store.MarkSyncSuccess(stale,"alice","stale",now,now.Add(time.Hour))},
				"failure":func()error{return store.MarkSyncFailure(stale,"alice","stale",now,true)},
				"legacy-success":func()error{return store.MarkSynced(stale,"alice",now)},
				"reconnect-flag":func()error{return store.MarkReconnectRequired(stale,"alice")},
			}{
				if err:=write();!errors.Is(err,calendarintegration.ErrSyncLeaseLost){t.Fatalf("%s stale write: %v",name,err)}
			}
			for _,table:=range []string{"private_events","schedule_projections"}{
				var n int
				if err:=db.QueryRowContext(ctx,"SELECT COUNT(*) FROM "+table).Scan(&n);err!=nil||n!=0{t.Fatalf("%s stale data: %d %v",table,n,err)}
			}
			current,err:=store.GetConnection(ctx,"alice")
			if scenario=="disconnect"{
				if !errors.Is(err,calendarintegration.ErrNotFound){t.Fatalf("connection resurrected: %v",err)}
			}else if err!=nil||current.SyncToken!=""||current.FailureCount!=0||current.ReconnectRequired{t.Fatalf("new connection changed: %+v %v",current,err)}
		})
	}
	if err:=store.SaveConnection(ctx,connection);err!=nil{t.Fatal(err)}
	active,err:=store.AcquireSync(ctx,"alice",time.Now().UTC(),time.Minute);if err!=nil{t.Fatal(err)}
	activeCtx:=calendarintegration.WithSyncLease(ctx,calendarintegration.SyncLease{UserID:"alice",ID:active.SyncLeaseID})
	if err:=store.MarkSyncSuccess(activeCtx,"alice","valid",now,now.Add(time.Hour));err!=nil{t.Fatal(err)}
	if _,err:=store.AcquireSync(ctx,"alice",time.Now().UTC(),time.Minute);err!=nil{t.Fatalf("successful sync did not release lease: %v",err)}
}
