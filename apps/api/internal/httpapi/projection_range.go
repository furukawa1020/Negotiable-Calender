package httpapi

import (
	"fmt"
	"net/url"
	"time"
)

const maxProjectionRange = 93 * 24 * time.Hour

func projectionRange(query url.Values, now time.Time) (string, time.Time, time.Time, error) {
	fail := func(message string) (string, time.Time, time.Time, error) {
		return "", time.Time{}, time.Time{}, fmt.Errorf("%s", message)
	}
	for key := range query {
		if key != "timezone" && key != "from" && key != "to" {
			return fail("invalid calendar query")
		}
	}
	for _, key := range []string{"timezone", "from", "to"} {
		if values, ok := query[key]; ok && (len(values) != 1 || values[0] == "") {
			return fail("invalid calendar query")
		}
	}
	zone := query.Get("timezone")
	if zone == "" {
		zone = "Asia/Tokyo"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return fail("invalid timezone")
	}
	local := now.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if raw := query.Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil || from.IsZero() {
			return fail("invalid from")
		}
		from = from.In(loc)
	}
	// Use a calendar day, not 24 hours: DST days can be 23 or 25 hours.
	to := from.AddDate(0, 0, 1)
	if raw := query.Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil || to.IsZero() {
			return fail("invalid to")
		}
	}
	if !to.After(from) || to.Sub(from) > maxProjectionRange {
		return fail("calendar range must be positive and at most 93 days")
	}
	return zone, from.UTC(), to.UTC(), nil
}
