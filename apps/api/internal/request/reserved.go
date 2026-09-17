package request

// ConfirmedRanges is used only server-side; it exposes no title or participant
// metadata to candidate generation and does not replace acceptance-time checks.
func ConfirmedRanges(values []CoordinationRequest) ([]ReservedRange, error) {
	ranges := []ReservedRange{}
	for _, value := range values {
		if value.Status != Accepted {
			continue
		}
		found := false
		for _, option := range value.Options {
			if option.ID != value.AcceptedOptionID {
				continue
			}
			found = true
			if option.Type != OptionMeeting {
				break
			}
			if option.StartAt == nil || option.EndAt == nil || !option.EndAt.After(*option.StartAt) {
				return nil, ErrAvailabilityChanged
			}
			ranges = append(ranges, ReservedRange{StartAt: *option.StartAt, EndAt: *option.EndAt})
			break
		}
		if !found {
			return nil, ErrAvailabilityChanged
		}
	}
	return ranges, nil
}
