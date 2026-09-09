package web

import (
	"sort"
	"time"

	"taskmanager/internal/store"
)

// calNav holds the computed previous / next / today anchors and a heading for
// the current calendar view.
type calNav struct {
	View    string
	Anchor  time.Time
	Prev    string
	Next    string
	Today   string
	Heading string
}

func buildCalNav(view string, anchor time.Time) calNav {
	today := time.Now()
	n := calNav{View: view, Anchor: anchor, Today: today.Format("2006-01-02")}
	switch view {
	case "day":
		n.Prev = anchor.AddDate(0, 0, -1).Format("2006-01-02")
		n.Next = anchor.AddDate(0, 0, 1).Format("2006-01-02")
		n.Heading = anchor.Format("Monday, January 2, 2006")
	case "week":
		start := startOfWeek(anchor)
		n.Prev = start.AddDate(0, 0, -7).Format("2006-01-02")
		n.Next = start.AddDate(0, 0, 7).Format("2006-01-02")
		end := start.AddDate(0, 0, 6)
		if start.Month() == end.Month() {
			n.Heading = start.Format("January 2") + "–" + end.Format("2, 2006")
		} else {
			n.Heading = start.Format("Jan 2") + " – " + end.Format("Jan 2, 2006")
		}
	default:
		n.Prev = anchor.AddDate(0, -1, 0).Format("2006-01-02")
		n.Next = anchor.AddDate(0, 1, 0).Format("2006-01-02")
		n.Heading = anchor.Format("January 2006")
	}
	return n
}

func startOfWeek(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	return d.AddDate(0, 0, -int(d.Weekday())) // Sunday-based
}

// calRange returns the [from, to) window a view needs to render.
func calRange(view string, anchor time.Time) (time.Time, time.Time) {
	switch view {
	case "day":
		start := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.Local)
		return start, start.AddDate(0, 0, 1)
	case "week":
		start := startOfWeek(anchor)
		return start, start.AddDate(0, 0, 7)
	default:
		first := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local)
		gridStart := first.AddDate(0, 0, -int(first.Weekday()))
		return gridStart, gridStart.AddDate(0, 0, 42)
	}
}

// ---- month ---------------------------------------------------------------

type monthDay struct {
	Date        time.Time
	InMonth     bool
	Today       bool
	Occurrences []store.Occurrence
}

func buildMonth(anchor time.Time, occ []store.Occurrence) [][]monthDay {
	gridStart, _ := calRange("month", anchor)
	today := time.Now()
	byDay := groupByDay(occ)

	weeks := make([][]monthDay, 0, 6)
	for w := 0; w < 6; w++ {
		row := make([]monthDay, 7)
		for d := 0; d < 7; d++ {
			day := gridStart.AddDate(0, 0, w*7+d)
			row[d] = monthDay{
				Date:        day,
				InMonth:     day.Month() == anchor.Month(),
				Today:       sameCalendarDay(day, today),
				Occurrences: byDay[day.Format("2006-01-02")],
			}
		}
		weeks = append(weeks, row)
	}
	return weeks
}

// ---- week / day (time grid) --------------------------------------------

type gridDay struct {
	Date   time.Time
	Today  bool
	AllDay []store.Occurrence
	Timed  []timedOcc
}

type timedOcc struct {
	store.Occurrence
	TopPct     float64
	HeightPct  float64
	Lane       int
	Lanes      int
	StartLabel string
	Continues  bool // started on a previous day
}

func buildGrid(view string, anchor time.Time, occ []store.Occurrence) []gridDay {
	var days []time.Time
	if view == "day" {
		days = []time.Time{time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.Local)}
	} else {
		start := startOfWeek(anchor)
		for i := 0; i < 7; i++ {
			days = append(days, start.AddDate(0, 0, i))
		}
	}
	today := time.Now()

	out := make([]gridDay, 0, len(days))
	for _, day := range days {
		next := day.AddDate(0, 0, 1)
		gd := gridDay{Date: day, Today: sameCalendarDay(day, today)}
		var timed []timedOcc
		for _, o := range occ {
			if !o.Start.Before(next) || !o.End.After(day) {
				continue
			}
			if o.AllDay || o.End.Sub(o.Start) >= 24*time.Hour {
				gd.AllDay = append(gd.AllDay, o)
				continue
			}
			s := o.Start
			if s.Before(day) {
				s = day
			}
			e := o.End
			if e.After(next) {
				e = next
			}
			startMin := s.Sub(day).Minutes()
			endMin := e.Sub(day).Minutes()
			if endMin-startMin < 20 {
				endMin = startMin + 20
			}
			timed = append(timed, timedOcc{
				Occurrence: o,
				TopPct:     startMin / 1440 * 100,
				HeightPct:  (endMin - startMin) / 1440 * 100,
				StartLabel: o.Start.Format("3:04"),
				Continues:  o.Start.Before(day),
			})
		}
		packLanes(timed)
		gd.Timed = timed
		out = append(out, gd)
	}
	return out
}

// packLanes assigns overlapping timed occurrences to side-by-side lanes.
func packLanes(items []timedOcc) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].TopPct < items[j].TopPct })
	var laneEnds []float64
	for i := range items {
		top := items[i].TopPct
		bottom := top + items[i].HeightPct
		placed := -1
		for l, end := range laneEnds {
			if top >= end-0.01 {
				placed = l
				laneEnds[l] = bottom
				break
			}
		}
		if placed < 0 {
			placed = len(laneEnds)
			laneEnds = append(laneEnds, bottom)
		}
		items[i].Lane = placed
	}
	lanes := len(laneEnds)
	if lanes == 0 {
		lanes = 1
	}
	// Second pass: real overlap groups could be narrower, but a single column
	// count keeps the math simple and readable.
	for i := range items {
		items[i].Lanes = lanes
	}
}

// ---- helpers ----------------------------------------------------------

func groupByDay(occ []store.Occurrence) map[string][]store.Occurrence {
	m := map[string][]store.Occurrence{}
	for _, o := range occ {
		day := time.Date(o.Start.Year(), o.Start.Month(), o.Start.Day(), 0, 0, 0, 0, time.Local)
		last := o.End
		// An end of exactly midnight doesn't occupy that final day.
		if last.After(o.Start) && last.Hour() == 0 && last.Minute() == 0 {
			last = last.Add(-time.Minute)
		}
		for i := 0; i < 62 && !day.After(last); i++ {
			key := day.Format("2006-01-02")
			m[key] = append(m[key], o)
			day = day.AddDate(0, 0, 1)
		}
	}
	return m
}

func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func parseAnchor(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
}

// hoursOfDay is the row labels for the time grid.
func hoursOfDay() []string {
	out := make([]string, 24)
	for h := 0; h < 24; h++ {
		t := time.Date(2000, 1, 1, h, 0, 0, 0, time.UTC)
		out[h] = t.Format("3 PM")
	}
	return out
}
