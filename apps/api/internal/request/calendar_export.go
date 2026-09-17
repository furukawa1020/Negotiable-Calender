package request

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// CalendarExport contains only the coordination request, never source calendar details.
func CalendarExport(value CoordinationRequest) (string, error) {
	if value.Status != Accepted || value.AcceptedOptionID == "" || value.ID == "" || value.UpdatedAt.IsZero() {
		return "", fmt.Errorf("confirmed meeting required")
	}
	var selected *Option
	for i := range value.Options {
		if value.Options[i].ID == value.AcceptedOptionID {
			selected = &value.Options[i]
			break
		}
	}
	if selected == nil || selected.RequestID != value.ID || selected.Type != OptionMeeting || selected.Validate() != nil {
		return "", fmt.Errorf("confirmed meeting required")
	}
	uid := sha256.Sum256([]byte(value.OrganizationID + ":" + value.ID))
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Negotiable Calendar//Coordination//EN", "CALSCALE:GREGORIAN", "BEGIN:VEVENT",
		fmt.Sprintf("UID:%x@negotiable-calendar", uid), "DTSTAMP:" + value.UpdatedAt.UTC().Format("20060102T150405Z"),
		"DTSTART:" + selected.StartAt.UTC().Format("20060102T150405Z"), "DTEND:" + selected.EndAt.UTC().Format("20060102T150405Z"),
		"SUMMARY:" + calendarText(value.Title), "STATUS:CONFIRMED", "CLASS:PRIVATE", "TRANSP:OPAQUE", "END:VEVENT", "END:VCALENDAR"}
	for i, line := range lines {
		lines[i] = foldCalendarLine(line)
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

func calendarText(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	var result strings.Builder
	for _, r := range value {
		switch r {
		case '\\', ';', ',':
			result.WriteByte('\\')
			result.WriteRune(r)
		case '\n':
			result.WriteString(`\n`)
		default:
			if r >= 32 && r != 127 {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

// RFC 5545 folds at 75 octets without splitting a UTF-8 code point.
func foldCalendarLine(value string) string {
	var result strings.Builder
	width := 0
	for _, r := range value {
		text := string(r)
		if width+len(text) > 75 {
			result.WriteString("\r\n ")
			width = 1
		}
		result.WriteString(text)
		width += len(text)
	}
	return result.String()
}
