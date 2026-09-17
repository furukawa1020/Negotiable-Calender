package httpapi

import (
	"errors"
	"net/http"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) exportConfirmedCalendar(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	userID := request.Header.Get("X-Demo-User-ID")
	if userID == "" {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "request identity is required"})
		return
	}
	value, err := api.requests.GetForUser(request.Context(), request.PathValue("requestId"), userID)
	if errors.Is(err, coordinationrequest.ErrNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "request not found"})
		return
	}
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "unable to load request"})
		return
	}
	if userID != value.RequesterUserID && userID != value.TargetUserID {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "request not found"})
		return
	}
	contents, err := coordinationrequest.CalendarExport(value)
	if err != nil {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "confirmed meeting required"})
		return
	}
	response.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	response.Header().Set("Content-Disposition", `attachment; filename="negotiable-meeting.ics"`)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(contents))
}
