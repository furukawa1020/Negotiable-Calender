package request

import (
	"testing"
	"time"
)

func TestOptionTypeOwnsItsPayload(t *testing.T) {
	now := time.Now().UTC()
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	for _, kind := range []OptionType{OptionMeeting, OptionAsync, OptionDelegate, OptionDecline} {
		for _, field := range []string{"valid", "start", "end", "response", "delegate"} {
			t.Run(string(kind)+"/"+field, func(t *testing.T) {
				o := Option{ID: "o", RequestID: "r", Type: kind, CreatedAt: now}
				if kind == OptionMeeting {
					o.StartAt, o.EndAt = &start, &end
				}
				if kind == OptionDelegate {
					o.DelegateUserID = "recipient"
				}
				switch field {
				case "start":
					o.StartAt = &start
				case "end":
					o.EndAt = &end
				case "response":
					o.ResponseBy = &end
				case "delegate":
					o.DelegateUserID = "recipient"
				}
				valid := field == "valid" || ((field == "start" || field == "end") && kind == OptionMeeting) || (field == "response" && kind == OptionAsync) || (field == "delegate" && kind == OptionDelegate)
				if err := o.Validate(); (err == nil) != valid {
					t.Fatalf("valid=%v error=%v", valid, err)
				}
			})
		}
	}
	for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		deadline := now.Add(offset)
		o := Option{ID: "o", RequestID: "r", Type: OptionAsync, CreatedAt: now, ResponseBy: &deadline}
		if err := o.Validate(); (err == nil) != (offset > 0) {
			t.Fatalf("offset=%v error=%v", offset, err)
		}
	}
}
