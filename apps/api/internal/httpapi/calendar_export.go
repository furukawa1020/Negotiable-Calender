package httpapi

import (
	"net/http"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (api *API) exportConfirmedCalendar(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	value, ok := api.loadWorkspaceRequest(response, request)
	if !ok {
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
