package calendar

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	_ "time/tzdata" // Keep IANA conversion available in minimal production images.
)

var errCalendarEvidence = errors.New("invalid calendar time evidence")

const googleBusyCursorPrefix = "gcal-busy-v2."

// This envelope is stored only where opaque sync tokens were already stored. Old
// raw tokens cannot establish that cached all-day ranges used calendar timezones.
type googleBusyCursor struct {
	TimeZone string `json:"timeZone"`
	Token    string `json:"token"`
}

func decodeGoogleBusyCursor(value string) googleBusyCursor {
	if !strings.HasPrefix(value, googleBusyCursorPrefix) {
		return googleBusyCursor{}
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, googleBusyCursorPrefix))
	var cursor googleBusyCursor
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Token == "" {
		return googleBusyCursor{}
	}
	if _, err := calendarLocation(cursor.TimeZone); err != nil {
		return googleBusyCursor{}
	}
	return cursor
}

func encodeGoogleBusyCursor(zone, token string) string {
	data, _ := json.Marshal(googleBusyCursor{TimeZone: zone, Token: token})
	return googleBusyCursorPrefix + base64.RawURLEncoding.EncodeToString(data)
}

func calendarLocation(zone string) (*time.Location, error) {
	// Empty and Local otherwise use host-dependent interpretations.
	if zone == "" || zone == "Local" {
		return nil, errCalendarEvidence
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, errCalendarEvidence
	}
	return location, nil
}

type googleEventTime struct{ DateTime, Date string }

func googleBusyRange(start, end googleEventTime, location *time.Location) (time.Time, time.Time, error) {
	var from, to time.Time
	var firstErr, lastErr error
	switch {
	case start.DateTime != "" && end.DateTime != "" && start.Date == "" && end.Date == "":
		from, firstErr = time.Parse(time.RFC3339, start.DateTime)
		to, lastErr = time.Parse(time.RFC3339, end.DateTime)
	case start.Date != "" && end.Date != "" && start.DateTime == "" && end.DateTime == "" && location != nil:
		// Parse both dates independently: an all-day range can be 23 or 25 hours
		// at a daylight-saving boundary. The end date is exclusive.
		from, firstErr = time.ParseInLocation("2006-01-02", start.Date, location)
		to, lastErr = time.ParseInLocation("2006-01-02", end.Date, location)
		if from.Format("2006-01-02") != start.Date || to.Format("2006-01-02") != end.Date {
			return time.Time{}, time.Time{}, errCalendarEvidence
		}
	default:
		return time.Time{}, time.Time{}, errCalendarEvidence
	}
	if firstErr != nil || lastErr != nil || from.IsZero() || !to.After(from) {
		return time.Time{}, time.Time{}, errCalendarEvidence
	}
	return from.UTC(), to.UTC(), nil
}
