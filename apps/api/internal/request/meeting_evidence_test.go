package request

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMeetingLifecycleRejectsAmbiguousEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	for _, corruption := range []string{"duplicate", "conflicting-duplicate", "reversed-duplicate", "foreign-request", "missing", "wrong-type", "missing-start", "invalid-created", "blank-request"} {
		t.Run(corruption, func(t *testing.T) {
			value := CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Accepted, AcceptedOptionID: "o", DeadlineAt: end,
				Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: now}}}
			switch corruption {
			case "duplicate", "conflicting-duplicate", "reversed-duplicate":
				other := value.Options[0]
				if corruption != "duplicate" {
					other.ProposedByUserID = "bob"
				}
				value.Options = append(value.Options, other)
				if corruption == "reversed-duplicate" {
					value.Options[0], value.Options[1] = value.Options[1], value.Options[0]
				}
			case "foreign-request":
				value.Options[0].RequestID = "other"
			case "missing":
				value.Options = nil
			case "wrong-type":
				value.Options[0].Type = OptionAsync
			case "missing-start":
				value.Options[0].StartAt = nil
			case "invalid-created":
				value.Options[0].CreatedAt = time.Time{}
			case "blank-request":
				value.ID, value.Options[0].RequestID = "", ""
			}
			if _, err := ConfirmableMeeting(value, "o", now); !errors.Is(err, ErrCandidateInvalid) {
				t.Errorf("confirmable: %v", err)
			}
			if err := AuthorizeConfirmation(value, "bob", "o"); !errors.Is(err, ErrCandidateInvalid) {
				t.Errorf("authorization: %v", err)
			}
			if err := AuthorizeConfirmation(value, "outsider", "o"); !errors.Is(err, ErrNotFound) {
				t.Errorf("outsider: %v", err)
			}
			for _, state := range []Status{Accepted, Cancelled} {
				value.Status = state
				if err := ValidateConfirmedCancellation(value, "alice", "o", now); !errors.Is(err, ErrCancellationInvalid) {
					t.Errorf("cancel %s: %v", state, err)
				}
			}
		})
	}
}

func TestProposalReplayRequiresUniqueOwnedValidEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	start, end := now.Add(time.Hour), now.Add(90*time.Minute)
	for _, corruption := range []string{"duplicate", "foreign-request", "invalid-created", "missing-end"} {
		t.Run(corruption, func(t *testing.T) {
			value := CoordinationRequest{ID: "r", OrganizationID: "org", RequesterUserID: "alice", TargetUserID: "bob", Status: Suggested, DurationMinutes: 30, DeadlineAt: end}
			if _, _, err := PrepareProposal(&value, "bob", "org", start, end, now); err != nil {
				t.Fatal(err)
			}
			switch corruption {
			case "duplicate":
				value.Options = append(value.Options, value.Options[0])
			case "foreign-request":
				value.Options[0].RequestID = "other"
			case "invalid-created":
				value.Options[0].CreatedAt = time.Time{}
			case "missing-end":
				value.Options[0].EndAt = nil
			}
			before := value
			before.Options = append([]Option(nil), value.Options...)
			option, replay, err := PrepareProposal(&value, "bob", "org", start, end, now.Add(48*time.Hour))
			if !errors.Is(err, ErrProposalConflict) || replay || option.ID != "" {
				t.Errorf("invalid replay: %v %v %v", option, replay, err)
			}
			if !reflect.DeepEqual(value, before) {
				t.Fatal("rejection mutated request")
			}
		})
	}
}

func TestValidMeetingEvidenceReplayDoesNotRequireFutureTime(t *testing.T) {
	now := time.Now().UTC()
	start, end := now.Add(-2*time.Hour), now.Add(-time.Hour)
	value := CoordinationRequest{ID: "r", RequesterUserID: "alice", TargetUserID: "bob", Status: Cancelled, AcceptedOptionID: "o",
		Options: []Option{{ID: "o", RequestID: "r", Type: OptionMeeting, StartAt: &start, EndAt: &end, CreatedAt: start.Add(-time.Hour)}}}
	if err := AuthorizeConfirmation(value, "bob", "o"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfirmedCancellation(value, "alice", "o", now); !errors.Is(err, ErrAlreadyCancelled) {
		t.Fatal(err)
	}
}
