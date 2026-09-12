package firestorestore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/auth"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type userRecord struct {
	ID, Email, DisplayName, AvatarURL, Timezone string
	CreatedAt, UpdatedAt                        time.Time
}
type organizationRecord struct {
	ID, Name             string
	CreatedAt, UpdatedAt time.Time
}
type membershipRecord struct {
	ID, OrganizationID, UserID string
	Role                       organization.Role
	CreatedAt                  time.Time
}
type identityRecord struct {
	Provider, Subject, UserID, Email string
	CreatedAt, UpdatedAt             time.Time
}

func hashID(value []byte) string { return hex.EncodeToString(value) }
func randomID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic("secure random source unavailable")
	}
	return prefix + "-" + hex.EncodeToString(value)
}

func (store *Auth) CreateFlow(ctx context.Context, value auth.Flow) error {
	_, err := store.Client.Collection("oauthFlows").Doc(value.ID).Create(ctx, value)
	return err
}
func (store *Auth) ConsumeFlow(ctx context.Context, id string, stateHash []byte, now time.Time) (auth.Flow, error) {
	ref := store.Client.Collection("oauthFlows").Doc(id)
	var value auth.Flow
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if !bytes.Equal(value.StateHash, stateHash) || !value.ExpiresAt.After(now) {
			return auth.ErrNotFound
		}
		return tx.Delete(ref)
	})
	if firestoreNotFound(err) {
		return auth.Flow{}, auth.ErrNotFound
	}
	return value, err
}

func (store *Auth) UpsertGoogleIdentity(ctx context.Context, profile auth.Profile, now time.Time) (auth.Identity, error) {
	ref := store.Client.Collection("authIdentities").Doc("google-" + safeDigest(profile.Subject))
	identityRecordValue := identityRecord{}
	if doc, err := ref.Get(ctx); err == nil {
		if err := doc.DataTo(&identityRecordValue); err != nil {
			return auth.Identity{}, err
		}
	} else if !firestoreNotFound(err) {
		return auth.Identity{}, err
	}
	priorIdentityUserID := identityRecordValue.UserID
	userID := identityRecordValue.UserID
	if userID == "" {
		matches, err := store.Client.Collection("users").Where("Email", "==", profile.Email).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return auth.Identity{}, err
		}
		if len(matches) > 0 {
			userID = matches[0].Ref.ID
		} else {
			userID = "user-" + safeDigest(profile.Subject)
		}
	}
	// A completed deletion never reuses the old user ID. New sign-in may
	// create a fresh account; a pending deletion cannot be bypassed.
	if doc, err := store.accountDeletionRef(userID).Get(ctx); err == nil {
		var deletion accountDeletion
		if err := doc.DataTo(&deletion); err != nil {
			return auth.Identity{}, err
		}
		if deletion.Phase != "complete" {
			return auth.Identity{}, errAccountDeleting
		}
		userID = randomID("user")
	} else if !firestoreNotFound(err) {
		return auth.Identity{}, err
	}
	displayName := profile.DisplayName
	if displayName == "" {
		displayName = profile.Email
	}
	userRef := store.Client.Collection("users").Doc(userID)
	user := userRecord{ID: userID, Email: profile.Email, DisplayName: displayName, AvatarURL: profile.AvatarURL, Timezone: "Asia/Tokyo", CreatedAt: now, UpdatedAt: now}
	if existing, err := userRef.Get(ctx); err == nil {
		var prior userRecord
		if existing.DataTo(&prior) == nil {
			user.CreatedAt = prior.CreatedAt
			if prior.Timezone != "" {
				user.Timezone = prior.Timezone
			}
		}
	}
	identityRecordValue = identityRecord{Provider: "google", Subject: profile.Subject, UserID: userID, Email: profile.Email, CreatedAt: now, UpdatedAt: now}
	if old, err := ref.Get(ctx); err == nil {
		var prior identityRecord
		if old.DataTo(&prior) == nil {
			identityRecordValue.CreatedAt = prior.CreatedAt
		}
	}
	err := store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		current, err := tx.Get(ref)
		currentUserID := ""
		if err == nil {
			var existing identityRecord
			if err := current.DataTo(&existing); err != nil {
				return err
			}
			currentUserID = existing.UserID
		} else if !firestoreNotFound(err) {
			return err
		}
		// Avoid competing post-deletion sign-ins creating orphan accounts.
		if currentUserID != priorIdentityUserID {
			return errAccountDeleting
		}
		if err := tx.Set(userRef, user); err != nil {
			return err
		}
		return tx.Set(ref, identityRecordValue)
	})
	if err != nil {
		return auth.Identity{}, err
	}
	workspaces, err := store.Organization().ListWorkspaces(ctx, userID)
	if err != nil {
		return auth.Identity{}, err
	}
	if len(workspaces) == 0 {
		orgID := "organization-" + safeDigest(userID)
		workspace := organization.Workspace{ID: orgID, Name: displayName + " Workspace", Role: organization.Owner}
		if err := store.createWorkspace(ctx, userID, workspace, now); err != nil {
			return auth.Identity{}, err
		}
		workspaces = []organization.Workspace{workspace}
	}
	workspace := workspaces[0]
	return auth.Identity{UserID: userID, OrganizationID: workspace.ID, Email: profile.Email, DisplayName: displayName, AvatarURL: profile.AvatarURL, Role: string(workspace.Role)}, nil
}

func (store *Auth) createWorkspace(ctx context.Context, userID string, value organization.Workspace, now time.Time) error {
	return store.fencedWrite(ctx, userID, func(tx *firestore.Transaction) error {
		if err := tx.Set(store.Client.Collection("organizations").Doc(value.ID), organizationRecord{ID: value.ID, Name: value.Name, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		member := membershipRecord{ID: randomID("membership"), OrganizationID: value.ID, UserID: userID, Role: value.Role, CreatedAt: now}
		if err := tx.Set(store.Client.Collection("organizations").Doc(value.ID).Collection("members").Doc(userID), member); err != nil {
			return err
		}
		return tx.Set(store.Client.Collection("users").Doc(userID).Collection("workspaces").Doc(value.ID), value)
	})
}
func (store *Auth) CreateSession(ctx context.Context, value auth.Session) error {
	return store.fencedWrite(ctx, value.UserID, func(tx *firestore.Transaction) error {
		return tx.Create(store.Client.Collection("authSessions").Doc(hashID(value.TokenHash)), value)
	})
}
func (store *Auth) GetSession(ctx context.Context, tokenHash []byte, now time.Time) (auth.Identity, error) {
	var session auth.Session
	doc, err := store.Client.Collection("authSessions").Doc(hashID(tokenHash)).Get(ctx)
	if firestoreNotFound(err) {
		return auth.Identity{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.Identity{}, err
	}
	if err = doc.DataTo(&session); err != nil {
		return auth.Identity{}, err
	}
	if !session.ExpiresAt.After(now) {
		return auth.Identity{}, auth.ErrNotFound
	}
	if deleting, err := store.accountIsDeleting(ctx, session.UserID); err != nil {
		return auth.Identity{}, err
	} else if deleting {
		return auth.Identity{}, auth.ErrNotFound
	}
	var user userRecord
	if doc, err = store.Client.Collection("users").Doc(session.UserID).Get(ctx); err != nil {
		return auth.Identity{}, auth.ErrNotFound
	}
	if err = doc.DataTo(&user); err != nil {
		return auth.Identity{}, err
	}
	var member membershipRecord
	if doc, err = store.Client.Collection("organizations").Doc(session.OrganizationID).Collection("members").Doc(session.UserID).Get(ctx); err != nil {
		return auth.Identity{}, auth.ErrNotFound
	}
	if err = doc.DataTo(&member); err != nil {
		return auth.Identity{}, err
	}
	return auth.Identity{UserID: user.ID, OrganizationID: session.OrganizationID, Email: user.Email, DisplayName: user.DisplayName, AvatarURL: user.AvatarURL, Role: string(member.Role)}, nil
}
func (store *Auth) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := store.Client.Collection("authSessions").Doc(hashID(tokenHash)).Delete(ctx)
	return err
}
func (store *Auth) DeleteAccount(ctx context.Context, userID string) error {
	deletion, err := store.beginAccountDeletion(ctx, userID)
	if err != nil {
		return err
	}
	if deletion.Phase == "complete" {
		return nil
	}
	userRef := store.Client.Collection("users").Doc(userID)
	workspaces := make([]organization.Workspace, 0, len(deletion.OrganizationIDs))
	for _, id := range deletion.OrganizationIDs {
		workspaces = append(workspaces, organization.Workspace{ID: id})
	}
	requests := store.Client.Collection("coordinationRequests")
	requestIter := requests.Documents(ctx)
	requestIDs := map[string]bool{}
	for {
		doc, nextErr := requestIter.Next()
		if errors.Is(nextErr, iterator.Done) {
			break
		}
		if nextErr != nil {
			requestIter.Stop()
			return nextErr
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			requestIter.Stop()
			return err
		}
		owned := value.RequesterUserID == userID || value.TargetUserID == userID || value.DelegatedUserID == userID
		for _, option := range value.Options {
			owned = owned || option.DelegateUserID == userID
		}
		if owned {
			requestIDs[value.ID] = true
			if _, err := doc.Ref.Delete(ctx); err != nil {
				requestIter.Stop()
				return err
			}
		}
	}
	requestIter.Stop()
	for _, workspace := range workspaces {
		audits := store.Client.Collection("organizations").Doc(workspace.ID).Collection("auditLogs")
		iter := audits.Documents(ctx)
		for {
			doc, nextErr := iter.Next()
			if errors.Is(nextErr, iterator.Done) {
				break
			}
			if nextErr != nil {
				iter.Stop()
				return nextErr
			}
			var event audit.Event
			if err := doc.DataTo(&event); err != nil {
				iter.Stop()
				return err
			}
			if event.ActorUserID == userID || requestIDs[event.ResourceID] {
				if _, err := doc.Ref.Delete(ctx); err != nil {
					iter.Stop()
					return err
				}
			}
		}
		iter.Stop()
	}
	for _, collection := range []string{"manualOverrides", "scheduleProjections", "notifications", "privateEvents", "workspaces", "projectionControls"} {
		if err := deleteCollection(ctx, store.Client, userRef.Collection(collection), 200); err != nil {
			return fmt.Errorf("delete account %s: %w", collection, err)
		}
	}
	for _, query := range []firestore.Query{
		store.Client.Collection("authSessions").Where("UserID", "==", userID),
		store.Client.Collection("authIdentities").Where("UserID", "==", userID),
		store.Client.Collection("calendarOAuthFlows").Where("UserID", "==", userID),
		store.Client.Collection("organizationInvitations").Where("InvitedBy", "==", userID),
	} {
		if err := deleteQuery(ctx, query); err != nil {
			return err
		}
	}
	if _, err := store.Client.Collection("sharingPolicies").Doc(userID).Delete(ctx); err != nil {
		return err
	}
	if _, err := store.Client.Collection("calendarConnections").Doc(userID).Delete(ctx); err != nil {
		return err
	}
	for _, workspace := range workspaces {
		members := store.Client.Collection("organizations").Doc(workspace.ID).Collection("members")
		if _, err := members.Doc(userID).Delete(ctx); err != nil {
			return err
		}
		docs, err := members.Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			if _, err := store.Client.Collection("organizations").Doc(workspace.ID).Delete(ctx); err != nil {
				return err
			}
		}
	}
	return store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := tx.Delete(userRef); err != nil {
			return err
		}
		// Keep only a non-PII lifecycle marker; stale user IDs remain fenced.
		return tx.Set(store.accountDeletionRef(userID), accountDeletion{Phase: "complete", StartedAt: deletion.StartedAt})
	})
}

func deleteQuery(ctx context.Context, query firestore.Query) error {
	iter := query.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := doc.Ref.Delete(ctx); err != nil {
			return err
		}
	}
}

func safeDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func (store *Organization) ListPeople(ctx context.Context, organizationID string) ([]organization.Person, error) {
	iter := store.Client.Collection("organizations").Doc(organizationID).Collection("members").Documents(ctx)
	defer iter.Stop()
	values := []organization.Person{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var member membershipRecord
		if err := doc.DataTo(&member); err != nil {
			return nil, err
		}
		if member.Role == organization.Member {
			continue
		}
		var user userRecord
		u, err := store.Client.Collection("users").Doc(member.UserID).Get(ctx)
		if err != nil {
			return nil, err
		}
		if err := u.DataTo(&user); err != nil {
			return nil, err
		}
		values = append(values, organization.Person{ID: user.ID, DisplayName: user.DisplayName, AvatarURL: user.AvatarURL, Timezone: user.Timezone, Role: member.Role})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].DisplayName == values[j].DisplayName {
			return values[i].ID < values[j].ID
		}
		return values[i].DisplayName < values[j].DisplayName
	})
	return values, nil
}
func (store *Organization) IsMember(ctx context.Context, organizationID, userID string) (bool, error) {
	_, err := store.Client.Collection("organizations").Doc(organizationID).Collection("members").Doc(userID).Get(ctx)
	if firestoreNotFound(err) {
		return false, nil
	}
	return err == nil, err
}
func (store *Organization) UserTimezone(ctx context.Context, userID string) (string, error) {
	doc, err := store.Client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		return "", err
	}
	var user userRecord
	if err := doc.DataTo(&user); err != nil {
		return "", err
	}
	return user.Timezone, nil
}

func (store *Organization) CreateInvitation(ctx context.Context, value organization.Invitation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	var member membershipRecord
	doc, err := store.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(value.InvitedBy).Get(ctx)
	if err != nil {
		return organization.ErrForbidden
	}
	if err := doc.DataTo(&member); err != nil || !organization.CanInvite(member.Role, value.Role) {
		return organization.ErrForbidden
	}
	event := audit.Event{ID: randomID("audit"), OrganizationID: value.OrganizationID, ActorUserID: value.InvitedBy, Action: audit.InvitationCreated, ResourceType: "invitation", ResourceID: value.ID, CreatedAt: value.CreatedAt}
	batch := store.Client.Batch()
	batch.Create(store.Client.Collection("organizationInvitations").Doc(hashID(value.TokenHash)), value)
	batch.Create(store.Client.Collection("organizations").Doc(value.OrganizationID).Collection("auditLogs").Doc(event.ID), event)
	_, err = batch.Commit(ctx)
	return err
}
func (store *Organization) PreviewInvitation(ctx context.Context, token []byte, now time.Time) (organization.InvitationPreview, error) {
	var value organization.Invitation
	doc, err := store.Client.Collection("organizationInvitations").Doc(hashID(token)).Get(ctx)
	if err != nil {
		return organization.InvitationPreview{}, organization.ErrInvitationNotFound
	}
	if err := doc.DataTo(&value); err != nil || !value.ExpiresAt.After(now) {
		return organization.InvitationPreview{}, organization.ErrInvitationNotFound
	}
	var org organizationRecord
	doc, err = store.Client.Collection("organizations").Doc(value.OrganizationID).Get(ctx)
	if err != nil {
		return organization.InvitationPreview{}, organization.ErrInvitationNotFound
	}
	if err := doc.DataTo(&org); err != nil {
		return organization.InvitationPreview{}, err
	}
	return organization.InvitationPreview{ID: value.ID, OrganizationID: org.ID, OrganizationName: org.Name, Role: value.Role, ExpiresAt: value.ExpiresAt}, nil
}
func (store *Organization) AcceptInvitation(ctx context.Context, token []byte, userID string, now time.Time) (organization.Workspace, error) {
	var workspace organization.Workspace
	if len(token) != 32 || userID == "" {
		return workspace, organization.ErrInvitationNotFound
	}
	invitationRef := store.Client.Collection("organizationInvitations").Doc(hashID(token))
	userRef := store.Client.Collection("users").Doc(userID)
	memberID, auditID := randomID("membership"), randomID("audit")
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		if err := store.guardAccountActive(ctx, tx, userID); err != nil {
			return err
		}
		doc, err := tx.Get(invitationRef)
		if err != nil {
			return err
		}
		var invitation organization.Invitation
		if err := doc.DataTo(&invitation); err != nil {
			return err
		}
		if invitation.Validate() != nil || !invitation.ExpiresAt.After(now) {
			return organization.ErrInvitationNotFound
		}
		orgRef := store.Client.Collection("organizations").Doc(invitation.OrganizationID)
		doc, err = tx.Get(orgRef)
		if err != nil {
			return err
		}
		var org organizationRecord
		if err := doc.DataTo(&org); err != nil {
			return err
		}
		if _, err := tx.Get(userRef); err != nil {
			return err
		}
		memberRef := orgRef.Collection("members").Doc(userID)
		member := membershipRecord{ID: memberID, OrganizationID: orgRef.ID, UserID: userID, Role: invitation.Role, CreatedAt: now}
		existing, err := tx.Get(memberRef)
		newMember := firestoreNotFound(err)
		if err != nil && !newMember {
			return err
		}
		if !newMember {
			if err := existing.DataTo(&member); err != nil {
				return err
			}
			if !member.Role.Valid() {
				return organization.ErrForbidden
			}
		}
		workspace = organization.Workspace{ID: orgRef.ID, Name: org.Name, Role: member.Role}
		if newMember {
			if err := tx.Create(memberRef, member); err != nil {
				return err
			}
		}
		if err := tx.Set(userRef.Collection("workspaces").Doc(orgRef.ID), workspace); err != nil {
			return err
		}
		if err := tx.Delete(invitationRef); err != nil {
			return err
		}
		event := audit.Event{ID: auditID, OrganizationID: orgRef.ID, ActorUserID: userID, Action: audit.InvitationAccepted, ResourceType: "invitation", ResourceID: invitation.ID, CreatedAt: now}
		return tx.Create(orgRef.Collection("auditLogs").Doc(auditID), event)
	})
	if firestoreNotFound(err) {
		return organization.Workspace{}, organization.ErrInvitationNotFound
	}
	if err != nil {
		return organization.Workspace{}, err
	}
	return workspace, nil
}
func (store *Organization) ListWorkspaces(ctx context.Context, userID string) ([]organization.Workspace, error) {
	iter := store.Client.Collection("users").Doc(userID).Collection("workspaces").Documents(ctx)
	defer iter.Stop()
	values := []organization.Workspace{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var value organization.Workspace
		if err := doc.DataTo(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Name == values[j].Name {
			return values[i].ID < values[j].ID
		}
		return values[i].Name < values[j].Name
	})
	return values, nil
}
func (store *Organization) SwitchWorkspace(ctx context.Context, sessionHash []byte, userID, organizationID string, now time.Time) (organization.Workspace, error) {
	var workspace organization.Workspace
	if len(sessionHash) != 32 || userID == "" || organizationID == "" {
		return workspace, organization.ErrForbidden
	}
	orgRef := store.Client.Collection("organizations").Doc(organizationID)
	sessionRef := store.Client.Collection("authSessions").Doc(hashID(sessionHash))
	auditID := randomID("audit")
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(orgRef.Collection("members").Doc(userID))
		if err != nil {
			return err
		}
		var member membershipRecord
		if err := doc.DataTo(&member); err != nil {
			return err
		}
		if member.UserID != userID || member.OrganizationID != organizationID || !member.Role.Valid() {
			return organization.ErrForbidden
		}
		doc, err = tx.Get(orgRef)
		if err != nil {
			return err
		}
		var org organizationRecord
		if err := doc.DataTo(&org); err != nil {
			return err
		}
		doc, err = tx.Get(sessionRef)
		if err != nil {
			return err
		}
		var session auth.Session
		if err := doc.DataTo(&session); err != nil {
			return err
		}
		if session.UserID != userID || !session.ExpiresAt.After(now) {
			return organization.ErrForbidden
		}
		workspace = organization.Workspace{ID: organizationID, Name: org.Name, Role: member.Role}
		if err := tx.Update(sessionRef, []firestore.Update{{Path: "OrganizationID", Value: organizationID}}); err != nil {
			return err
		}
		event := audit.Event{ID: auditID, OrganizationID: organizationID, ActorUserID: userID, Action: audit.WorkspaceSwitched, ResourceType: "workspace", ResourceID: organizationID, CreatedAt: now}
		return tx.Create(orgRef.Collection("auditLogs").Doc(auditID), event)
	})
	if firestoreNotFound(err) {
		return organization.Workspace{}, organization.ErrForbidden
	}
	if err != nil {
		return organization.Workspace{}, err
	}
	return workspace, nil
}

func SeedDemo(ctx context.Context, backend *Backend, now time.Time) (bool, error) {
	marker := backend.Client.Collection("_system").Doc("demo-seed-v1")
	if _, err := marker.Get(ctx); err == nil {
		return false, nil
	} else if !firestoreNotFound(err) {
		return false, err
	}
	users := []userRecord{
		{ID: "demo-manager", Email: "manager@example.invalid", DisplayName: "山田 太郎", Timezone: "Asia/Tokyo", CreatedAt: now, UpdatedAt: now},
		{ID: "demo-member", Email: "member@example.invalid", DisplayName: "佐藤 花子", Timezone: "Asia/Tokyo", CreatedAt: now, UpdatedAt: now},
	}
	org := organizationRecord{ID: "demo-org", Name: "Product Studio", CreatedAt: now, UpdatedAt: now}
	batch := backend.Client.Batch()
	batch.Create(marker, map[string]any{"createdAt": now, "version": 1})
	batch.Set(backend.Client.Collection("organizations").Doc(org.ID), org)
	roles := []organization.Role{organization.Manager, organization.Member}
	for index, user := range users {
		batch.Set(backend.Client.Collection("users").Doc(user.ID), user)
		member := membershipRecord{ID: "demo-membership-" + user.ID, OrganizationID: org.ID, UserID: user.ID, Role: roles[index], CreatedAt: now}
		batch.Set(backend.Client.Collection("organizations").Doc(org.ID).Collection("members").Doc(user.ID), member)
		batch.Set(backend.Client.Collection("users").Doc(user.ID).Collection("workspaces").Doc(org.ID), organization.Workspace{ID: org.ID, Name: org.Name, Role: roles[index]})
	}
	if _, err := batch.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
