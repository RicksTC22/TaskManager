package parse

import (
	"strings"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"X-WR-CALNAME:Work\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:evt-1@example.com\r\n" +
	"SUMMARY:Team standup\r\n" +
	"DESCRIPTION:Daily sync\\, keep it short\r\n" +
	"LOCATION:Room 2\r\n" +
	"DTSTART:20260910T140000Z\r\n" +
	"DTEND:20260910T143000Z\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=6\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:evt-2\r\n" +
	"SUMMARY:Conference\r\n" +
	"DTSTART;VALUE=DATE:20260912\r\n" +
	"DTEND;VALUE=DATE:20260914\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	cal, err := ParseICS(strings.NewReader(sampleICS))
	if err != nil {
		t.Fatal(err)
	}
	if cal.Name != "Work" {
		t.Errorf("calendar name = %q", cal.Name)
	}
	if len(cal.Events) != 2 {
		t.Fatalf("got %d events", len(cal.Events))
	}

	e := cal.Events[0]
	if e.UID != "evt-1@example.com" || e.Summary != "Team standup" {
		t.Errorf("event 0 fields: %+v", e)
	}
	if e.Description != "Daily sync, keep it short" {
		t.Errorf("description not unescaped: %q", e.Description)
	}
	if e.AllDay {
		t.Error("event 0 should be timed")
	}
	if e.End.Sub(e.Start) != 30*time.Minute {
		t.Errorf("event 0 duration = %v", e.End.Sub(e.Start))
	}
	if e.RRule != "FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=6" {
		t.Errorf("rrule = %q", e.RRule)
	}

	c := cal.Events[1]
	if !c.AllDay {
		t.Error("event 1 should be all-day")
	}
}

func TestExpandRRuleWeekly(t *testing.T) {
	start := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local) // Monday
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local)

	got := ExpandRRule(start, "FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=6", from, to, 100)
	// COUNT=6 -> Mon9/7, Wed9/9, Fri9/11, Mon9/14, Wed9/16, Fri9/18 (all within window)
	if len(got) != 6 {
		t.Fatalf("got %d occurrences: %v", len(got), got)
	}
	if !got[0].Equal(start) {
		t.Errorf("first occ = %v", got[0])
	}
	if got[1].Weekday() != time.Wednesday || got[1].Hour() != 9 {
		t.Errorf("second occ = %v", got[1])
	}
	if !got[5].Equal(time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)) {
		t.Errorf("last occ = %v", got[5])
	}
}

func TestExpandRRuleDailyUntil(t *testing.T) {
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local)
	from := start
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	got := ExpandRRule(start, "FREQ=DAILY;INTERVAL=2;UNTIL=20260910T000000Z", from, to, 100)
	// 9/1, 9/3, 9/5, 9/7, 9/9
	if len(got) != 5 {
		t.Fatalf("got %d: %v", len(got), got)
	}
}

func TestExpandRRuleNoRule(t *testing.T) {
	start := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	in := ExpandRRule(start, "", start.AddDate(0, 0, -1), start.AddDate(0, 0, 1), 10)
	if len(in) != 1 {
		t.Fatalf("plain event in range should yield 1, got %d", len(in))
	}
	out := ExpandRRule(start, "", start.AddDate(0, 0, 5), start.AddDate(0, 0, 10), 10)
	if len(out) != 0 {
		t.Fatalf("plain event out of range should yield 0, got %d", len(out))
	}
}
