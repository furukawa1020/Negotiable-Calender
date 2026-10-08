package request

// meetingEvidence validates immutable selection evidence, without checking the
// current time or calendar availability. This must run before replay success as
// well as new transitions: a retry may be old, but cannot be ambiguous or corrupt.
func meetingEvidence(value CoordinationRequest, optionID string) (Option, error) {
	var selected Option
	found := false
	for _, option := range value.Options {
		if option.ID != optionID {
			continue
		}
		if found || option.RequestID != value.ID || option.Type != OptionMeeting || option.Validate() != nil {
			return Option{}, ErrCandidateInvalid
		}
		selected, found = option, true
	}
	if !found {
		return Option{}, ErrCandidateInvalid
	}
	return selected, nil
}
