package projection

import (
	calendarintegration "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/calendar"
)

// BoundToSource discards unproven coverage and never extends provider freshness.
func BoundToSource(values []ScheduleProjection, state calendarintegration.SourceState) []ScheduleProjection {
	if !state.Managed {
		return values
	}
	result := make([]ScheduleProjection, 0, len(values))
	for _, value := range values {
		if !state.Snapshot.Covers(value.StartAt, value.EndAt) {
			continue
		}
		deadline := state.Snapshot.ObservedAt.Add(calendarintegration.SourceMaxAge)
		if deadline.Before(value.ExpiresAt) {
			value.ExpiresAt = deadline
		}
		result = append(result, value)
	}
	return result
}
