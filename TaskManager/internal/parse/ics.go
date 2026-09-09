package parse

import (
	"bufio"
	"io"
	"strings"
	"time"
)

// ICSCalendar is a parsed .ics file.
type ICSCalendar struct {
	Name   string
	Events []ICSEvent
}

// ICSEvent is one VEVENT.
type ICSEvent struct {
	UID         string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	RRule       string
}

// ParseICS reads an iCalendar stream. Times are converted to the local zone;
// TZID values are honoured when the zone database can resolve them, otherwise
// the wall-clock time is kept as local. Unknown properties are ignored.
func ParseICS(r io.Reader) (*ICSCalendar, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}

	cal := &ICSCalendar{}
	var cur *ICSEvent
	var startHadTime, endHadTime bool
	var duration time.Duration

	for _, ln := range lines {
		name, params, value := splitLine(ln)
		upper := strings.ToUpper(name)

		switch upper {
		case "BEGIN":
			if strings.EqualFold(value, "VEVENT") {
				cur = &ICSEvent{}
				startHadTime, endHadTime, duration = false, false, 0
			}
			continue
		case "END":
			if strings.EqualFold(value, "VEVENT") && cur != nil {
				finishEvent(cur, startHadTime, endHadTime, duration)
				cal.Events = append(cal.Events, *cur)
				cur = nil
			}
			continue
		}

		if cur == nil {
			switch upper {
			case "X-WR-CALNAME", "NAME":
				if cal.Name == "" {
					cal.Name = unescapeText(value)
				}
			}
			continue
		}

		switch upper {
		case "UID":
			cur.UID = value
		case "SUMMARY":
			cur.Summary = unescapeText(value)
		case "DESCRIPTION":
			cur.Description = unescapeText(value)
		case "LOCATION":
			cur.Location = unescapeText(value)
		case "RRULE":
			cur.RRule = value
		case "DTSTART":
			t, hadTime, allDay := parseICSTime(params, value)
			cur.Start, startHadTime = t, hadTime
			if allDay {
				cur.AllDay = true
			}
		case "DTEND":
			t, hadTime, _ := parseICSTime(params, value)
			cur.End, endHadTime = t, hadTime
		case "DURATION":
			duration = parseICSDuration(value)
		}
	}
	return cal, nil
}

func finishEvent(e *ICSEvent, startHadTime, endHadTime bool, dur time.Duration) {
	if e.End.IsZero() {
		switch {
		case dur > 0:
			e.End = e.Start.Add(dur)
		case e.AllDay:
			e.End = e.Start.AddDate(0, 0, 1)
		default:
			e.End = e.Start.Add(time.Hour)
		}
	}
	if e.AllDay || (!startHadTime && !endHadTime) {
		e.AllDay = true
	}
}

// unfold reads all lines, joining RFC 5545 continuation lines (those beginning
// with a space or tab) onto the preceding line.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

// splitLine breaks "DTSTART;TZID=Europe/Paris:20260101T100000" into
// name="DTSTART", params={TZID: Europe/Paris}, value="20260101T100000".
func splitLine(line string) (name string, params map[string]string, value string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line, nil, ""
	}
	left, value := line[:colon], line[colon+1:]
	parts := strings.Split(left, ";")
	name = parts[0]
	params = map[string]string{}
	for _, p := range parts[1:] {
		if eq := strings.IndexByte(p, '='); eq >= 0 {
			params[strings.ToUpper(p[:eq])] = strings.Trim(p[eq+1:], `"`)
		}
	}
	return name, params, value
}

func parseICSTime(params map[string]string, value string) (t time.Time, hadTime, allDay bool) {
	value = strings.TrimSpace(value)
	if params["VALUE"] == "DATE" || len(value) == 8 {
		if d, err := time.ParseInLocation("20060102", value, time.Local); err == nil {
			return d, false, true
		}
	}
	if strings.HasSuffix(value, "Z") {
		if d, err := time.ParseInLocation("20060102T150405Z", value, time.UTC); err == nil {
			return d.In(time.Local), true, false
		}
	}
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	if d, err := time.ParseInLocation("20060102T150405", value, loc); err == nil {
		return d.In(time.Local), true, false
	}
	if d, err := time.ParseInLocation("20060102", value, time.Local); err == nil {
		return d, false, true
	}
	return time.Time{}, false, false
}

// parseICSDuration handles the common subset: PnDTnHnMnS / PTnHnM / PnW.
func parseICSDuration(v string) time.Duration {
	v = strings.TrimSpace(strings.ToUpper(v))
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0
	}
	v = v[1:]
	var total time.Duration
	inTime := false
	num := ""
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inTime = true
			num = ""
		default:
			n := atoiSafe(num)
			num = ""
			switch r {
			case 'W':
				total += time.Duration(n) * 7 * 24 * time.Hour
			case 'D':
				total += time.Duration(n) * 24 * time.Hour
			case 'H':
				total += time.Duration(n) * time.Hour
			case 'M':
				if inTime {
					total += time.Duration(n) * time.Minute
				}
			case 'S':
				total += time.Duration(n) * time.Second
			}
		}
	}
	if neg {
		return -total
	}
	return total
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func unescapeText(s string) string {
	repl := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return repl.Replace(s)
}
