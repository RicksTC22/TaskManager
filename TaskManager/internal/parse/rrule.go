package parse

import (
	"strings"
	"time"
)

var icsWeekday = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// ExpandRRule returns the occurrence start times of a recurring event whose
// first instance is at start, limited to [from, to) and to maxOccurrences. It
// supports FREQ=DAILY|WEEKLY|MONTHLY|YEARLY with INTERVAL, COUNT, UNTIL and
// (for weekly) BYDAY — enough for display of typical imported calendars.
func ExpandRRule(start time.Time, rule string, from, to time.Time, maxOccurrences int) []time.Time {
	if rule == "" {
		if !start.Before(from) && start.Before(to) {
			return []time.Time{start}
		}
		return nil
	}

	freq := ""
	interval := 1
	count := 0
	var until time.Time
	var byday []time.Weekday

	for _, part := range strings.Split(rule, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := strings.ToUpper(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1])
		switch key {
		case "FREQ":
			freq = strings.ToUpper(val)
		case "INTERVAL":
			if n := atoiSafe(val); n > 0 {
				interval = n
			}
		case "COUNT":
			count = atoiSafe(val)
		case "UNTIL":
			until = parseUntil(val)
		case "BYDAY":
			for _, d := range strings.Split(val, ",") {
				d = strings.ToUpper(strings.TrimSpace(d))
				if len(d) >= 2 {
					if wd, ok := icsWeekday[d[len(d)-2:]]; ok {
						byday = append(byday, wd)
					}
				}
			}
		}
	}
	if maxOccurrences <= 0 {
		maxOccurrences = 500
	}

	var out []time.Time
	emitted := 0
	add := func(t time.Time) bool {
		if count > 0 && emitted >= count {
			return false
		}
		if !until.IsZero() && t.After(until) {
			return false
		}
		emitted++
		if !t.Before(from) && t.Before(to) {
			out = append(out, t)
		}
		return true
	}

	switch freq {
	case "WEEKLY":
		if len(byday) == 0 {
			byday = []time.Weekday{start.Weekday()}
		}
		weekStart := start.AddDate(0, 0, -int(start.Weekday())) // Sunday of start's week
		for w := 0; w < maxOccurrences; w++ {
			base := weekStart.AddDate(0, 0, 7*w*interval)
			if base.After(to) {
				break
			}
			stop := false
			for d := 0; d < 7; d++ {
				day := base.AddDate(0, 0, d)
				if !containsWeekday(byday, day.Weekday()) {
					continue
				}
				occ := time.Date(day.Year(), day.Month(), day.Day(),
					start.Hour(), start.Minute(), start.Second(), 0, start.Location())
				if occ.Before(start) {
					continue
				}
				if !add(occ) {
					stop = true
					break
				}
			}
			if stop || len(out) >= maxOccurrences {
				break
			}
		}
	case "DAILY":
		for i := 0; i < maxOccurrences; i++ {
			occ := start.AddDate(0, 0, i*interval)
			if occ.After(to) {
				break
			}
			if !add(occ) {
				break
			}
		}
	case "MONTHLY":
		for i := 0; i < maxOccurrences; i++ {
			occ := start.AddDate(0, i*interval, 0)
			if occ.After(to) {
				break
			}
			if !add(occ) {
				break
			}
		}
	case "YEARLY":
		for i := 0; i < maxOccurrences; i++ {
			occ := start.AddDate(i*interval, 0, 0)
			if occ.After(to) {
				break
			}
			if !add(occ) {
				break
			}
		}
	default:
		if !start.Before(from) && start.Before(to) {
			out = append(out, start)
		}
	}
	return out
}

func parseUntil(v string) time.Time {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, err := time.ParseInLocation(layout, v, time.UTC); err == nil {
			return t.In(time.Local)
		}
	}
	return time.Time{}
}

func containsWeekday(list []time.Weekday, wd time.Weekday) bool {
	for _, w := range list {
		if w == wd {
			return true
		}
	}
	return false
}
