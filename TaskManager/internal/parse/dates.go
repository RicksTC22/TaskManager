package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const isoDate = "2006-01-02"

var (
	isoRe    = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	slashRe  = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?$`)
	inDaysRe = regexp.MustCompile(`^in(\d{1,3})d$`)
)

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

var months = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// ParseDate turns a single date token into an ISO "YYYY-MM-DD" string, relative
// to now. It understands: today, tomorrow, tmrw, tonight, weekday names (the
// next such day), "in3d", ISO dates, M/D[/Y], and "sep 10" / "10 sep".
func ParseDate(token string, now time.Time) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(token))
	t = strings.TrimPrefix(t, "due:")
	t = strings.TrimPrefix(t, "@")
	t = strings.TrimSpace(t)
	if t == "" {
		return "", false
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch t {
	case "today", "tod", "tonight":
		return day.Format(isoDate), true
	case "tomorrow", "tmrw", "tmr", "tom":
		return day.AddDate(0, 0, 1).Format(isoDate), true
	case "yesterday":
		return day.AddDate(0, 0, -1).Format(isoDate), true
	case "nextweek":
		return day.AddDate(0, 0, 7).Format(isoDate), true
	}

	if m := inDaysRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return day.AddDate(0, 0, n).Format(isoDate), true
	}

	if wd, ok := weekdays[t]; ok {
		return nextWeekday(day, wd).Format(isoDate), true
	}
	if strings.HasPrefix(t, "next") {
		if wd, ok := weekdays[strings.TrimPrefix(t, "next")]; ok {
			return nextWeekday(day, wd).AddDate(0, 0, 7).Format(isoDate), true
		}
	}

	if m := isoRe.FindStringSubmatch(t); m != nil {
		if _, err := time.Parse(isoDate, t); err == nil {
			return t, true
		}
	}

	if m := slashRe.FindStringSubmatch(t); m != nil {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		year := now.Year()
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
			if year < 100 {
				year += 2000
			}
		}
		if valid(year, mo, d) {
			return fmt.Sprintf("%04d-%02d-%02d", year, mo, d), true
		}
	}

	// "sep 10" / "10 sep" / "september 10"
	fields := strings.FieldsFunc(t, func(r rune) bool { return r == ' ' || r == '-' })
	if len(fields) == 2 {
		if mo, ok := months[fields[0]]; ok {
			if d, err := strconv.Atoi(strings.TrimRight(fields[1], "stndrh")); err == nil {
				return resolveMonthDay(now, mo, d), true
			}
		}
		if mo, ok := months[fields[1]]; ok {
			if d, err := strconv.Atoi(strings.TrimRight(fields[0], "stndrh")); err == nil {
				return resolveMonthDay(now, mo, d), true
			}
		}
	}
	return "", false
}

func nextWeekday(from time.Time, wd time.Weekday) time.Time {
	delta := (int(wd) - int(from.Weekday()) + 7) % 7
	if delta == 0 {
		delta = 7
	}
	return from.AddDate(0, 0, delta)
}

func resolveMonthDay(now time.Time, mo time.Month, d int) string {
	year := now.Year()
	cand := time.Date(year, mo, d, 0, 0, 0, 0, now.Location())
	if cand.Before(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())) {
		cand = cand.AddDate(1, 0, 0)
	}
	return cand.Format(isoDate)
}

func valid(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return int(t.Month()) == m && t.Day() == d
}
