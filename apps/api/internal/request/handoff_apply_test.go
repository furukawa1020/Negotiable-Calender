package request

import (
	"reflect"
	"testing"
	"time"
)

func TestApplyHandoffRechecksAuthorizationAndState(t *testing.T) {
	now := time.Now().UTC()
	source := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour)}
	option := Option{ID: "o", RequestID: "r", Type: OptionAsync, CreatedAt: now, ResponseBy: &source.DeadlineAt}
	for _, scenario := range []string{"outsider", "self", "requester", "accepted", "expired", "empty-org", "already-handed-off"} {
		t.Run(scenario, func(t *testing.T) {
			value, actor, recipient := source, "bob", "carol"
			switch scenario {
			case "outsider":
				actor = "mallory"
			case "self":
				recipient = "bob"
			case "requester":
				recipient = "alice"
			case "accepted":
				value.Status = Accepted
			case "expired":
				value.DeadlineAt = now
			case "empty-org":
				value.OrganizationID = ""
			case "already-handed-off":
				value.DelegatedFromUserID = "bob"
				value.DelegatedUserID = "carol"
				value.TargetUserID = "carol"
			}
			before := value
			if ApplyHandoff(&value, actor, recipient, []Option{option}, now) == nil || !reflect.DeepEqual(value, before) {
				t.Fatal("invalid handoff mutated record")
			}
		})
	}
	if ApplyHandoff(nil, "bob", "carol", []Option{option}, now) == nil {
		t.Fatal("nil record accepted")
	}
	if err := ApplyHandoff(&source, "bob", "carol", []Option{option}, now); err != nil {
		t.Fatal(err)
	}
}
