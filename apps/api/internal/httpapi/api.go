package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/audit"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/notification"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/organization"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/projection"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type API struct {
	database      databasePinger
	policies      policy.Store
	projections   projection.Store
	organizations organization.Store
	requests      coordinationrequest.Store
	notifications notification.Store
	audits        audit.Store
	webOrigin     string
	logger        *slog.Logger
}

func New(database databasePinger, policies policy.Store, projections projection.Store, organizations organization.Store, requests coordinationrequest.Store, webOrigin string, logger *slog.Logger) http.Handler {
	return newAPI(database, policies, projections, organizations, requests, nil, nil, webOrigin, logger)
}

func NewWithNotifications(database databasePinger, policies policy.Store, projections projection.Store, organizations organization.Store, requests coordinationrequest.Store, notifications notification.Store, webOrigin string, logger *slog.Logger) http.Handler {
	return newAPI(database, policies, projections, organizations, requests, notifications, nil, webOrigin, logger)
}

func NewWithStores(database databasePinger, policies policy.Store, projections projection.Store, organizations organization.Store, requests coordinationrequest.Store, notifications notification.Store, audits audit.Store, webOrigin string, logger *slog.Logger) http.Handler {
	return newAPI(database, policies, projections, organizations, requests, notifications, audits, webOrigin, logger)
}

func newAPI(database databasePinger, policies policy.Store, projections projection.Store, organizations organization.Store, requests coordinationrequest.Store, notifications notification.Store, audits audit.Store, webOrigin string, logger *slog.Logger) http.Handler {
	api := &API{database: database, policies: policies, projections: projections, organizations: organizations, requests: requests, notifications: notifications, audits: audits, webOrigin: webOrigin, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("GET /api/v1/status", api.status)
	mux.HandleFunc("GET /api/v1/users/{userId}/sharing-policy", api.getSharingPolicy)
	mux.HandleFunc("PUT /api/v1/users/{userId}/sharing-policy", api.putSharingPolicy)
	mux.HandleFunc("GET /api/v1/users/{userId}/manual-overrides", api.listManualOverrides)
	mux.HandleFunc("POST /api/v1/users/{userId}/manual-overrides", api.createManualOverride)
	mux.HandleFunc("GET /api/v1/users/{userId}/export", api.exportUserData)
	mux.HandleFunc("GET /api/v1/people/{userId}/projection", api.getPublicProjection)
	mux.HandleFunc("GET /api/v1/people", api.listPeople)
	mux.HandleFunc("GET /api/v1/requests", api.listCoordinationRequests)
	mux.HandleFunc("POST /api/v1/requests", api.createCoordinationRequest)
	mux.HandleFunc("GET /api/v1/requests/{requestId}", api.getCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/accept", api.acceptCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/suggest", api.suggestCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/delegate", api.delegateCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/decline", api.declineCoordinationRequest)
	mux.HandleFunc("POST /api/v1/requests/{requestId}/cancel", api.cancelCoordinationRequest)
	mux.HandleFunc("GET /api/v1/notifications", api.listNotifications)
	mux.HandleFunc("POST /api/v1/notifications/{notificationId}/read", api.readNotification)
	mux.HandleFunc("GET /api/v1/audit-logs", api.listAuditLogs)
	mux.HandleFunc("OPTIONS /api/v1/{path...}", api.options)
	return api.middleware(mux)
}

func (api *API) exportUserData(response http.ResponseWriter, request *http.Request) {
	userID := request.PathValue("userId")
	if !api.requireSelf(response, request, userID) {
		return
	}

	requests, err := api.requests.ListForUser(request.Context(), userID)
	if err != nil {
		api.logger.Error("export coordination requests", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to export user data"})
		return
	}
	projections, err := api.projections.ListForUser(request.Context(), userID)
	if err != nil {
		api.logger.Error("export projections", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to export user data"})
		return
	}

	var sharingPolicy *policy.SharingPolicy
	value, err := api.policies.Get(request.Context(), userID)
	if err == nil {
		sharingPolicy = &value
	} else if !errors.Is(err, policy.ErrNotFound) {
		api.logger.Error("export sharing policy", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to export user data"})
		return
	}

	response.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="negotiable-calendar-%s.json"`, userID))
	writeJSON(response, http.StatusOK, map[string]any{
		"generatedAt": time.Now().UTC(),
		"userId":      userID,
		"requests":    requests,
		"policy":      sharingPolicy,
		"projections": projections,
	})
}

func (api *API) listAuditLogs(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	organizationID := request.Header.Get("X-Organization-ID")
	if userID == "" || organizationID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if !api.requireMembership(response, request, organizationID, userID) {
		return
	}
	if api.audits == nil {
		writeJSON(response, http.StatusOK, map[string]any{"auditLogs": []audit.Event{}})
		return
	}
	values, err := api.audits.List(request.Context(), organizationID)
	if err != nil {
		api.logger.Error("list audit logs", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load audit logs"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"auditLogs": values})
}

func (api *API) requireSelf(response http.ResponseWriter, request *http.Request, userID string) bool {
	authenticatedUserID := request.Header.Get("X-Demo-User-ID")
	if authenticatedUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return false
	}
	if authenticatedUserID != userID {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "access denied"})
		return false
	}
	return true
}

func (api *API) requireMembership(response http.ResponseWriter, request *http.Request, organizationID, userID string) bool {
	member, err := api.organizations.IsMember(request.Context(), organizationID, userID)
	if err != nil {
		api.logger.Error("check organization membership", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to authorize request"})
		return false
	}
	if !member {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "access denied"})
		return false
	}
	return true
}

func (api *API) recordAudit(ctx context.Context, actorUserID string, action audit.Action, requestID string) {
	if api.audits == nil {
		return
	}
	err := api.audits.Create(ctx, audit.Event{
		ID: newID("audit"), ActorUserID: actorUserID, Action: action,
		ResourceType: "request", ResourceID: requestID, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		api.logger.Error("create audit event", "action", action, "error", err)
	}
}

func (api *API) listNotifications(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if api.notifications == nil {
		writeJSON(response, http.StatusOK, map[string]any{"notifications": []notification.Notification{}})
		return
	}
	values, err := api.notifications.List(request.Context(), userID)
	if err != nil {
		api.logger.Error("list notifications", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load notifications"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"notifications": values})
}

func (api *API) readNotification(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if api.notifications == nil {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "notification not found"})
		return
	}
	updated, err := api.notifications.MarkRead(request.Context(), request.PathValue("notificationId"), userID, time.Now().UTC())
	if err != nil {
		api.logger.Error("mark notification read", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update notification"})
		return
	}
	if !updated {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "notification not found"})
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) notify(ctx context.Context, userID string, kind notification.Type, requestID, message string) {
	if api.notifications == nil {
		return
	}
	err := api.notifications.Create(ctx, notification.Notification{
		ID: newID("notification"), UserID: userID, Type: kind,
		RequestID: requestID, Message: message, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		api.logger.Error("create in-app notification", "type", kind, "error", err)
	}
}

type delegateCoordinationRequestInput struct {
	DelegateUserID string `json:"delegateUserId"`
}

func (api *API) delegateCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input delegateCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.DelegateUserID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "delegateUserId is required"})
		return
	}
	option := coordinationrequest.Option{
		ID: newID("option"), RequestID: request.PathValue("requestId"),
		Type: coordinationrequest.OptionDelegate, DelegateUserID: input.DelegateUserID,
		CreatedAt: time.Now().UTC(),
	}
	err := api.requests.Delegate(request.Context(), option.RequestID, targetUserID, option)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be delegated"})
		return
	}
	if err != nil {
		api.logger.Error("delegate coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to delegate request"})
		return
	}
	api.notify(request.Context(), targetUserID, notification.RequestDelegated, option.RequestID, "依頼を別のメンバーへ委譲しました。")
	api.recordAudit(request.Context(), targetUserID, audit.RequestDelegated, option.RequestID)
	writeJSON(response, http.StatusOK, map[string]any{
		"id": option.RequestID, "status": coordinationrequest.Delegated,
		"delegatedUserId": option.DelegateUserID,
	})
}

type suggestCoordinationRequestInput struct {
	StartAt time.Time `json:"startAt"`
	EndAt   time.Time `json:"endAt"`
}

func (api *API) suggestCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input suggestCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	startAt, endAt := input.StartAt.UTC(), input.EndAt.UTC()
	option := coordinationrequest.Option{
		ID: newID("option"), RequestID: request.PathValue("requestId"),
		Type: coordinationrequest.OptionMeeting, StartAt: &startAt, EndAt: &endAt, CreatedAt: now,
	}
	if !startAt.After(now) {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": "suggested time must be in the future"})
		return
	}
	if err := option.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid suggested option: %s", err)})
		return
	}
	err := api.requests.Suggest(request.Context(), option.RequestID, targetUserID, option)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be updated"})
		return
	}
	if err != nil {
		api.logger.Error("suggest coordination option", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to suggest option"})
		return
	}
	api.notify(request.Context(), targetUserID, notification.RequestChanged, option.RequestID, "依頼に別の時間候補を追加しました。")
	api.recordAudit(request.Context(), targetUserID, audit.RequestChanged, option.RequestID)
	writeJSON(response, http.StatusCreated, option)
}

type acceptCoordinationRequestInput struct {
	OptionID string `json:"optionId"`
}

func (api *API) acceptCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var input acceptCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.OptionID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "optionId is required"})
		return
	}
	api.respondToCoordinationRequest(response, request, targetUserID, coordinationrequest.Accepted, input.OptionID)
}

func (api *API) declineCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	targetUserID := request.Header.Get("X-Demo-User-ID")
	if targetUserID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	api.respondToCoordinationRequest(response, request, targetUserID, coordinationrequest.Declined, "")
}

func (api *API) respondToCoordinationRequest(response http.ResponseWriter, request *http.Request, targetUserID string, status coordinationrequest.Status, optionID string) {
	err := api.requests.Respond(request.Context(), request.PathValue("requestId"), targetUserID, status, optionID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request cannot be updated"})
		return
	}
	if err != nil {
		api.logger.Error("respond to coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update request"})
		return
	}
	kind, message := notification.RequestAccepted, "依頼の候補を承認しました。"
	if status == coordinationrequest.Declined {
		kind, message = notification.RequestDeclined, "依頼を辞退しました。"
	}
	api.notify(request.Context(), targetUserID, kind, request.PathValue("requestId"), message)
	auditAction := audit.RequestAccepted
	if status == coordinationrequest.Declined {
		auditAction = audit.RequestDeclined
	}
	api.recordAudit(request.Context(), targetUserID, auditAction, request.PathValue("requestId"))
	writeJSON(response, http.StatusOK, map[string]any{"id": request.PathValue("requestId"), "status": status, "acceptedOptionId": optionID})
}

func (api *API) listCoordinationRequests(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	var values []coordinationrequest.CoordinationRequest
	var err error
	switch request.URL.Query().Get("scope") {
	case "", "inbox":
		values, err = api.requests.ListForTarget(request.Context(), userID)
	case "sent":
		values, err = api.requests.ListForRequester(request.Context(), userID)
	default:
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid request scope"})
		return
	}
	if err != nil {
		api.logger.Error("list coordination requests", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load requests"})
		return
	}
	if values == nil {
		values = []coordinationrequest.CoordinationRequest{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"requests": values})
}

func (api *API) getCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	value, err := api.requests.GetForUser(request.Context(), request.PathValue("requestId"), userID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "coordination request not found"})
		return
	}
	if err != nil {
		api.logger.Error("get coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load request"})
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (api *API) cancelCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	requestID := request.PathValue("requestId")
	value, err := api.requests.GetForUser(request.Context(), requestID, userID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "coordination request not found"})
		return
	}
	if err != nil {
		api.logger.Error("load coordination request before cancellation", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to cancel request"})
		return
	}
	if value.RequesterUserID != userID {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "only the requester can cancel this request"})
		return
	}
	if err := api.requests.Cancel(request.Context(), requestID, userID); errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request is no longer cancellable"})
		return
	} else if err != nil {
		api.logger.Error("cancel coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to cancel request"})
		return
	}
	api.notify(request.Context(), value.TargetUserID, notification.RequestCancelled, requestID, "調整依頼がキャンセルされました。")
	api.recordAudit(request.Context(), userID, audit.RequestCancelled, requestID)
	writeJSON(response, http.StatusOK, map[string]any{"id": requestID, "status": coordinationrequest.Cancelled})
}

type createCoordinationRequestInput struct {
	TargetUserID    string                             `json:"targetUserId"`
	Type            coordinationrequest.Type           `json:"type"`
	Title           string                             `json:"title"`
	DurationMinutes int                                `json:"durationMinutes"`
	DeadlineAt      time.Time                          `json:"deadlineAt"`
	SyncPreference  coordinationrequest.SyncPreference `json:"syncPreference"`
	Priority        coordinationrequest.Priority       `json:"priority"`
}

func (api *API) createCoordinationRequest(response http.ResponseWriter, request *http.Request) {
	requesterID := request.Header.Get("X-Demo-User-ID")
	organizationID := request.Header.Get("X-Organization-ID")
	if requesterID == "" || organizationID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if !api.requireMembership(response, request, organizationID, requesterID) {
		return
	}
	var input createCoordinationRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	value := coordinationrequest.CoordinationRequest{
		ID: newID("request"), OrganizationID: organizationID,
		RequesterUserID: requesterID, TargetUserID: input.TargetUserID,
		Type: input.Type, Title: input.Title, DurationMinutes: input.DurationMinutes,
		DeadlineAt: input.DeadlineAt.UTC(), SyncPreference: input.SyncPreference,
		Priority: input.Priority, Status: coordinationrequest.Pending,
		Options: []coordinationrequest.Option{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid coordination request: %s", err)})
		return
	}
	if !api.requireMembership(response, request, organizationID, value.TargetUserID) {
		return
	}
	publicProjections, err := api.projections.List(request.Context(), value.TargetUserID, now, value.DeadlineAt)
	if err != nil {
		api.logger.Error("load projections for request candidates", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to generate request options"})
		return
	}
	options, err := coordinationrequest.GenerateCandidates(coordinationrequest.CandidateInput{
		Request: value, Projections: publicProjections, Now: now,
	})
	if err != nil {
		api.logger.Error("generate request candidates", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to generate request options"})
		return
	}
	value.Options = options
	value.Status = coordinationrequest.Suggested
	if err := api.requests.Create(request.Context(), value); err != nil {
		api.logger.Error("create coordination request", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create request"})
		return
	}
	api.notify(request.Context(), value.TargetUserID, notification.RequestReceived, value.ID, "新しい調整依頼が届きました。")
	api.recordAudit(request.Context(), value.RequesterUserID, audit.RequestCreated, value.ID)
	writeJSON(response, http.StatusCreated, value)
}

func (api *API) listPeople(response http.ResponseWriter, request *http.Request) {
	organizationID := request.URL.Query().Get("organizationId")
	if organizationID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "organizationId is required"})
		return
	}
	actorUserID := request.Header.Get("X-Demo-User-ID")
	headerOrganizationID := request.Header.Get("X-Organization-ID")
	if actorUserID == "" || headerOrganizationID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if headerOrganizationID != organizationID || !api.requireMembership(response, request, organizationID, actorUserID) {
		if headerOrganizationID != organizationID {
			writeJSON(response, http.StatusForbidden, map[string]string{"error": "access denied"})
		}
		return
	}
	people, err := api.organizations.ListPeople(request.Context(), organizationID)
	if err != nil {
		api.logger.Error("list organization people", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load people"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"people": people})
}

func (api *API) getPublicProjection(response http.ResponseWriter, request *http.Request) {
	actorUserID := request.Header.Get("X-Demo-User-ID")
	organizationID := request.Header.Get("X-Organization-ID")
	if actorUserID == "" || organizationID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	if !api.requireMembership(response, request, organizationID, actorUserID) || !api.requireMembership(response, request, organizationID, request.PathValue("userId")) {
		return
	}
	timezone := request.URL.Query().Get("timezone")
	if timezone == "" {
		timezone = "Asia/Tokyo"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid timezone"})
		return
	}
	localNow := time.Now().In(location)
	localStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	from := localStart.UTC()
	to := localStart.Add(24 * time.Hour).UTC()
	if raw := request.URL.Query().Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid from"})
			return
		}
		from = from.UTC()
	}
	if raw := request.URL.Query().Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid to"})
			return
		}
		to = to.UTC()
	}
	if !to.After(from) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "to must be after from"})
		return
	}
	view, err := api.projections.GetView(request.Context(), request.PathValue("userId"), timezone, from, to)
	if err != nil {
		api.logger.Error("get public projection", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load projection"})
		return
	}
	writeJSON(response, http.StatusOK, view)
}

type createOverrideRequest struct {
	StartAt   time.Time               `json:"startAt"`
	EndAt     time.Time               `json:"endAt"`
	State     policy.InteractionState `json:"state"`
	ExpiresAt time.Time               `json:"expiresAt"`
}

func (api *API) listManualOverrides(response http.ResponseWriter, request *http.Request) {
	if !api.requireSelf(response, request, request.PathValue("userId")) {
		return
	}
	values, err := api.policies.ListActiveOverrides(request.Context(), request.PathValue("userId"), time.Now().UTC())
	if err != nil {
		api.logger.Error("list manual overrides", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load manual overrides"})
		return
	}
	if values == nil {
		values = []policy.ManualOverride{}
	}
	writeJSON(response, http.StatusOK, map[string]any{"overrides": values})
}

func (api *API) createManualOverride(response http.ResponseWriter, request *http.Request) {
	if !api.requireSelf(response, request, request.PathValue("userId")) {
		return
	}
	var input createOverrideRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	now := time.Now().UTC()
	value := policy.ManualOverride{
		ID: newID("override"), UserID: request.PathValue("userId"),
		StartAt: input.StartAt, EndAt: input.EndAt, State: input.State,
		ExpiresAt: input.ExpiresAt, CreatedAt: now,
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid manual override: %s", err)})
		return
	}
	if err := api.policies.CreateOverride(request.Context(), value); err != nil {
		api.logger.Error("create manual override", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to create manual override"})
		return
	}
	writeJSON(response, http.StatusCreated, value)
}

func (api *API) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := api.database.PingContext(ctx); err != nil {
		api.logger.Warn("database readiness check failed", "error", err)
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) status(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{
		"product":      "Negotiable Calendar",
		"privacy_mode": "fail_closed",
		"status":       "operational",
	})
}

func (api *API) options(response http.ResponseWriter, _ *http.Request) {
	response.WriteHeader(http.StatusNoContent)
}

type updatePolicyRequest struct {
	Default      policy.InteractionState `json:"default"`
	WorkingHours []policy.WorkingWindow  `json:"workingHours"`
	Rules        []updateRuleRequest     `json:"rules"`
}

type updateRuleRequest struct {
	ConditionType string                  `json:"conditionType"`
	Condition     json.RawMessage         `json:"condition"`
	State         policy.InteractionState `json:"state"`
	Priority      int                     `json:"priority"`
	Enabled       bool                    `json:"enabled"`
}

func (api *API) getSharingPolicy(response http.ResponseWriter, request *http.Request) {
	if !api.requireSelf(response, request, request.PathValue("userId")) {
		return
	}
	value, err := api.policies.Get(request.Context(), request.PathValue("userId"))
	if errors.Is(err, policy.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "sharing policy not found"})
		return
	}
	if err != nil {
		api.logger.Error("get sharing policy", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load sharing policy"})
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func (api *API) putSharingPolicy(response http.ResponseWriter, request *http.Request) {
	if !api.requireSelf(response, request, request.PathValue("userId")) {
		return
	}
	var input updatePolicyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON value"})
		return
	}

	userID := request.PathValue("userId")
	now := time.Now().UTC()
	value, err := api.policies.Get(request.Context(), userID)
	if errors.Is(err, policy.ErrNotFound) {
		value = policy.SharingPolicy{ID: newID("policy"), UserID: userID, CreatedAt: now}
	} else if err != nil {
		api.logger.Error("load sharing policy before update", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update sharing policy"})
		return
	}
	value.Default = input.Default
	value.WorkingHours = input.WorkingHours
	value.Rules = make([]policy.Rule, 0, len(input.Rules))
	value.UpdatedAt = now
	for _, inputRule := range input.Rules {
		value.Rules = append(value.Rules, policy.Rule{
			ID: newID("rule"), PolicyID: value.ID,
			ConditionType: inputRule.ConditionType, Condition: inputRule.Condition,
			State: inputRule.State, Priority: inputRule.Priority, Enabled: inputRule.Enabled,
		})
	}
	if err := value.Validate(); err != nil {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]string{"error": fmt.Sprintf("invalid sharing policy: %s", err)})
		return
	}
	if err := api.policies.Upsert(request.Context(), value); err != nil {
		api.logger.Error("update sharing policy", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to update sharing policy"})
		return
	}
	writeJSON(response, http.StatusOK, value)
}

func newID(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("secure random source unavailable")
	}
	return fmt.Sprintf("%s-%x", prefix, bytes)
}

func (api *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cache-Control", "no-store")

		if api.webOrigin != "" && request.Header.Get("Origin") == api.webOrigin {
			response.Header().Set("Access-Control-Allow-Origin", api.webOrigin)
			response.Header().Set("Access-Control-Allow-Credentials", "true")
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Demo-User-ID, X-Organization-ID")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			response.Header().Set("Vary", "Origin")
		}

		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
