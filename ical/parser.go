package ical

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// Event represents a parsed VEVENT from an ICS feed
type Event struct {
	UID         string
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	RawLines    []string // Original ICS lines for this event block
}

// YearMonth returns "YYYY-MM" for this event's start date
func (e *Event) YearMonth() string {
	return fmt.Sprintf("%d-%02d", e.Start.Year(), e.Start.Month())
}

// ParseICS parses an ICS string and returns all VEVENTs
func ParseICS(data string) ([]Event, error) {
	var events []Event
	scanner := bufio.NewScanner(strings.NewReader(data))

	var inEvent bool
	var lines []string
	var props map[string]string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "BEGIN:VEVENT") {
			inEvent = true
			lines = []string{line}
			props = make(map[string]string)
			continue
		}

		if strings.HasPrefix(line, "END:VEVENT") {
			lines = append(lines, line)

			ev := Event{
				UID:         props["UID"],
				Summary:     props["SUMMARY"],
				Description: unfold(props["DESCRIPTION"]),
				Location:    props["LOCATION"],
				URL:         props["URL"],
				RawLines:    lines,
			}

			if dtstart, ok := props["DTSTART"]; ok {
				ev.Start = parseICalDate(dtstart)
			}
			if dtend, ok := props["DTEND"]; ok {
				ev.End = parseICalDate(dtend)
			}

			if !ev.Start.IsZero() {
				events = append(events, ev)
			}

			inEvent = false
			continue
		}

		if inEvent {
			lines = append(lines, line)

			// Handle line continuation (folded lines start with space or tab)
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				// Append to previous property value
				for k, v := range props {
					_ = v
					_ = k
				}
				// Find the last property we set and append
				// Simple approach: just track current key
				continue
			}

			// Parse property
			key, value := parseProperty(line)
			if key != "" {
				props[key] = value
			}
		}
	}

	return events, scanner.Err()
}

// parseProperty extracts key and value from an ICS property line
// Handles "KEY;PARAMS:VALUE" and "KEY:VALUE"
func parseProperty(line string) (string, string) {
	// Remove \r
	line = strings.TrimRight(line, "\r")

	// Find the first colon (but handle cases like DTSTART;VALUE=DATE:20250101)
	colonIdx := -1
	for i, ch := range line {
		if ch == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx < 0 {
		return "", ""
	}

	keyPart := line[:colonIdx]
	value := line[colonIdx+1:]

	// Strip parameters (e.g., DTSTART;TZID=Europe/Brussels -> DTSTART)
	key := keyPart
	if semiIdx := strings.Index(keyPart, ";"); semiIdx >= 0 {
		key = keyPart[:semiIdx]
	}

	return strings.TrimSpace(key), value
}

// parseICalDate parses various iCal date/datetime formats
func parseICalDate(s string) time.Time {
	s = strings.TrimSpace(s)

	// Try full datetime with Z (UTC)
	if t, err := time.Parse("20060102T150405Z", s); err == nil {
		return t
	}

	// Try full datetime without Z (local time)
	if t, err := time.Parse("20060102T150405", s); err == nil {
		return t
	}

	// Try date only
	if t, err := time.Parse("20060102", s); err == nil {
		return t
	}

	return time.Time{}
}

func unfold(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}

// GroupByMonth groups events by their YYYY-MM start date
func GroupByMonth(events []Event) map[string][]Event {
	result := make(map[string][]Event)
	for _, e := range events {
		ym := e.YearMonth()
		result[ym] = append(result[ym], e)
	}
	return result
}

// WrapICS wraps VEVENT blocks into a valid ICS file
func WrapICS(events []Event, prodID string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\n")
	b.WriteString("VERSION:2.0\n")
	b.WriteString(fmt.Sprintf("PRODID:%s\n", prodID))
	b.WriteString("CALSCALE:GREGORIAN\n")
	b.WriteString("METHOD:PUBLISH\n")
	for _, e := range events {
		for _, line := range e.RawLines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("END:VCALENDAR\n")
	return b.String()
}
