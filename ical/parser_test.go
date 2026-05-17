package ical

import (
	"testing"
	"time"
)

func TestParseICalDateDefaultsFloatingTimesToBrussels(t *testing.T) {
	got, allDay := parseICalDate("20260701T190000", nil)
	if allDay {
		t.Fatal("floating datetime parsed as all-day")
	}
	if got.Location().String() != defaultTimezone {
		t.Fatalf("location = %q, want %q", got.Location(), defaultTimezone)
	}
	if got.Format(time.RFC3339) != "2026-07-01T19:00:00+02:00" {
		t.Fatalf("time = %s", got.Format(time.RFC3339))
	}
}

func TestParseICalDateKeepsUTCInstantsButYearMonthUsesBrussels(t *testing.T) {
	start, allDay := parseICalDate("20260531T223000Z", nil)
	if allDay {
		t.Fatal("UTC datetime parsed as all-day")
	}
	event := Event{Start: start}
	if got := event.YearMonth(); got != "2026-06" {
		t.Fatalf("YearMonth() = %q, want 2026-06", got)
	}
}
