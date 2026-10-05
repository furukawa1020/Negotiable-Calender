package httpapi

import (
	"errors"
	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
	"net/http"
)

func writeCreationError(response http.ResponseWriter, err error) {
	if errors.Is(err, coordinationrequest.ErrCreationExpired) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request deadline or candidate time passed before saving; check deadline and retry", "code": coordinationrequest.ErrCreationExpired.Error()})
		return
	}
	status, message := http.StatusServiceUnavailable, "unable to confirm request creation; retry with the same key and payload"
	if errors.Is(err, coordinationrequest.ErrCreationConflict) {
		status, message = http.StatusConflict, "idempotency_key_conflict"
	} else if errors.Is(err, coordinationrequest.ErrCreationForbidden) {
		status, message = http.StatusForbidden, "request participants are not current organization members"
	}
	writeJSON(response, status, map[string]string{"error": message})
}
