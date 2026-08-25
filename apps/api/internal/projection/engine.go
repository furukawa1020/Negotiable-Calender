package projection

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/policy"
	"github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/privateevent"
)

const BucketSize = 15 * time.Minute

type GenerateInput struct {
	UserID          string
	Timezone        string
	From            time.Time
	To              time.Time
	Events          []privateevent.PrivateEvent
	Policy          policy.SharingPolicy
	ManualOverrides []policy.ManualOverride
	Now             time.Time
}

type Engine struct {
	ProjectionTTL time.Duration
}

func NewEngine() Engine {
	return Engine{ProjectionTTL: time.Hour}
}

func (e Engine) Generate(input GenerateInput) ([]ScheduleProjection, error) {
	if strings.TrimSpace(input.UserID) == "" {
		return nil, fmt.Errorf("user id is required")
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}
	if err := validRange(input.From, input.To); err != nil {
		return nil, err
	}
	if err := validUTC("now", input.Now); err != nil {
		return nil, err
	}
	if err := input.Policy.Validate(); err != nil {
		return nil, fmt.Errorf("invalid policy: %w", err)
	}
	if input.Policy.UserID != input.UserID {
		return nil, fmt.Errorf("policy user does not match projection user")
	}
	for _, event := range input.Events {
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("invalid private event: %w", err)
		}
		if event.UserID != input.UserID {
			return nil, fmt.Errorf("event user does not match projection user")
		}
	}
	for _, override := range input.ManualOverrides {
		if err := override.Validate(); err != nil {
			return nil, fmt.Errorf("invalid manual override: %w", err)
		}
		if override.UserID != input.UserID {
			return nil, fmt.Errorf("override user does not match projection user")
		}
	}

	start := floorBucket(input.From)
	end := ceilBucket(input.To)
	result := make([]ScheduleProjection, 0, int(end.Sub(start)/BucketSize))
	for cursor := start; cursor.Before(end); cursor = cursor.Add(BucketSize) {
		bucketEnd := cursor.Add(BucketSize)
		state := input.Policy.Default
		if !insideWorkingHours(cursor, bucketEnd, location, input.Policy.WorkingHours) {
			state = closedState()
		} else {
			for _, event := range input.Events {
				if overlaps(cursor, bucketEnd, event.StartAt, event.EndAt) {
					eventState := stateForEvent(event, input.Policy.Rules)
					state = restrictive(state, eventState)
				}
			}
		}
		for _, override := range input.ManualOverrides {
			if overlaps(cursor, bucketEnd, override.StartAt, override.EndAt) {
				state = override.State
			}
		}
		result = append(result, ScheduleProjection{
			ID:     fmt.Sprintf("%s:%d", input.UserID, cursor.Unix()),
			UserID: input.UserID, StartAt: cursor, EndAt: bucketEnd, State: state,
			ExpectedResponseBucket: responseBucket(state), GeneratedAt: input.Now,
			ExpiresAt: input.Now.Add(e.ProjectionTTL),
		})
	}
	return result, nil
}

func insideWorkingHours(startAt, endAt time.Time, location *time.Location, windows []policy.WorkingWindow) bool {
	localStart := startAt.In(location)
	localEnd := endAt.In(location)
	if localStart.Weekday() != localEnd.Weekday() {
		return false
	}
	startMinute := localStart.Hour()*60 + localStart.Minute()
	endMinute := localEnd.Hour()*60 + localEnd.Minute()
	for _, window := range windows {
		if window.Weekday != localStart.Weekday() {
			continue
		}
		if startMinute < window.StartMinute {
			continue
		}
		if endMinute <= window.EndMinute {
			return true
		}
	}
	return false
}

type ruleCondition struct {
	BusyStatus string `json:"busyStatus"`
	CalendarID string `json:"calendarId"`
}

func stateForEvent(event privateevent.PrivateEvent, rules []policy.Rule) policy.InteractionState {
	bestRank := -1
	bestPriority := -1
	state := fallbackEventState(event.BusyStatus)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		matched, rank := ruleMatches(rule, event)
		if !matched {
			continue
		}
		if rank < bestRank {
			continue
		}
		if rank == bestRank {
			if rule.Priority < bestPriority {
				continue
			}
		}
		state = rule.State
		bestRank = rank
		bestPriority = rule.Priority
	}
	return state
}

func ruleMatches(rule policy.Rule, event privateevent.PrivateEvent) (bool, int) {
	var condition ruleCondition
	if err := json.Unmarshal(rule.Condition, &condition); err != nil {
		return false, 0
	}
	switch rule.ConditionType {
	case "organization":
		return true, 1
	case "calendar":
		return condition.CalendarID == event.CalendarID, 2
	case "event":
		return condition.BusyStatus == string(event.BusyStatus), 3
	default:
		return false, 0
	}
}

func fallbackEventState(status privateevent.BusyStatus) policy.InteractionState {
	if status == privateevent.Free {
		return policy.InteractionState{Availability: policy.Available, Interruptibility: policy.InterruptOpen, Requestability: policy.RequestOpen, Reschedulability: policy.RescheduleHigh}
	}
	return closedState()
}

func closedState() policy.InteractionState {
	return policy.InteractionState{Availability: policy.Unavailable, Interruptibility: policy.DoNotInterrupt, Requestability: policy.RequestClosed, Reschedulability: policy.RescheduleFixed}
}

func responseBucket(state policy.InteractionState) string {
	if state.Requestability == policy.RequestOpen {
		return "within_window"
	}
	if state.Requestability == policy.AsyncOnly {
		return "within_window"
	}
	return "unknown"
}

func overlaps(firstStart, firstEnd, secondStart, secondEnd time.Time) bool {
	if !firstStart.Before(secondEnd) {
		return false
	}
	return secondStart.Before(firstEnd)
}

func floorBucket(value time.Time) time.Time {
	seconds := int64(BucketSize / time.Second)
	return time.Unix(value.Unix()/seconds*seconds, 0).UTC()
}

func ceilBucket(value time.Time) time.Time {
	floor := floorBucket(value)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(BucketSize)
}

func restrictive(left, right policy.InteractionState) policy.InteractionState {
	return policy.InteractionState{
		Availability:     restrictiveAvailability(left.Availability, right.Availability),
		Interruptibility: restrictiveInterruptibility(left.Interruptibility, right.Interruptibility),
		Requestability:   restrictiveRequestability(left.Requestability, right.Requestability),
		Reschedulability: restrictiveReschedulability(left.Reschedulability, right.Reschedulability),
	}
}

func restrictiveAvailability(left, right policy.Availability) policy.Availability {
	order := map[policy.Availability]int{policy.Available: 0, policy.Limited: 1, policy.Unknown: 2, policy.Unavailable: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}

func restrictiveInterruptibility(left, right policy.Interruptibility) policy.Interruptibility {
	order := map[policy.Interruptibility]int{policy.InterruptOpen: 0, policy.InterruptNormal: 1, policy.UrgentOnly: 2, policy.DoNotInterrupt: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}

func restrictiveRequestability(left, right policy.Requestability) policy.Requestability {
	order := map[policy.Requestability]int{policy.RequestOpen: 0, policy.AsyncOnly: 1, policy.RequestLater: 2, policy.RequestClosed: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}

func restrictiveReschedulability(left, right policy.Reschedulability) policy.Reschedulability {
	order := map[policy.Reschedulability]int{policy.RescheduleHigh: 0, policy.RescheduleMedium: 1, policy.RescheduleLow: 2, policy.RescheduleFixed: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}
