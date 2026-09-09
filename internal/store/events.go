package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"taskmanager/internal/parse"
)

const (
	stampDate = "2006-01-02"
	stampTime = "2006-01-02T15:04"
)

// Event is a stored calendar event.
type Event struct {
	ID          int64
	CalendarID  int64
	UID         string
	Title       string
	Description string
	Location    string
	StartsAt    string
	EndsAt      string
	AllDay      bool
	RRule       string
	Source      string

	Calendar *Calendar
}

// Start parses the stored start into a time.
func (e *Event) Start() time.Time { return parseStamp(e.StartsAt) }

// End parses the stored end into a time.
func (e *Event) End() time.Time { return parseStamp(e.EndsAt) }

// Occurrence is one concrete dated instance for rendering on the calendar.
type Occurrence struct {
	Kind         string // "event" | "task"
	Ref          string // event id, or entry slug for tasks
	Title        string
	Start        time.Time
	End          time.Time
	AllDay       bool
	Done         bool
	Priority     int
	Color        string
	CalendarID   int64
	CalendarName string
}

// EventInput is the payload for creating or updating an event.
type EventInput struct {
	CalendarID  int64
	Title       string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	RRule       string
	Source      string
	UID         string
}

func parseStamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if t, err := time.ParseInLocation(stampTime, s, time.Local); err == nil {
		return t
	}
	if t, err := time.ParseInLocation(stampDate, s, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

func formatStamp(t time.Time, allDay bool) string {
	if allDay {
		return t.Format(stampDate)
	}
	return t.Format(stampTime)
}

// EventByID loads one event owned by the user.
func (db *DB) EventByID(userID, id int64) (*Event, error) {
	var e Event
	var allDay int
	err := db.sql.QueryRow(`SELECT id, calendar_id, uid, title, description, location,
		starts_at, ends_at, all_day, rrule, source
		FROM events WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&e.ID, &e.CalendarID, &e.UID, &e.Title, &e.Description, &e.Location,
			&e.StartsAt, &e.EndsAt, &allDay, &e.RRule, &e.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.AllDay = allDay != 0
	if cal, cerr := db.CalendarByID(userID, e.CalendarID); cerr == nil {
		e.Calendar = cal
	}
	return &e, nil
}

// CreateEvent inserts an event.
func (db *DB) CreateEvent(userID int64, in EventInput) (*Event, error) {
	if in.CalendarID == 0 {
		return nil, errors.New("calendar required")
	}
	if in.End.Before(in.Start) {
		in.End = in.Start
		if in.AllDay {
			in.End = in.Start.AddDate(0, 0, 1)
		} else {
			in.End = in.Start.Add(time.Hour)
		}
	}
	src := in.Source
	if src == "" {
		src = "manual"
	}
	res, err := db.sql.Exec(`INSERT INTO events
		(user_id, calendar_id, uid, title, description, location, starts_at, ends_at, all_day, rrule, source, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, in.CalendarID, in.UID, strings.TrimSpace(in.Title), in.Description, in.Location,
		formatStamp(in.Start, in.AllDay), formatStamp(in.End, in.AllDay), boolToInt(in.AllDay),
		in.RRule, src, now(), now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return db.EventByID(userID, id)
}

// UpdateEvent rewrites an event's fields.
func (db *DB) UpdateEvent(userID, id int64, in EventInput) (*Event, error) {
	if in.End.Before(in.Start) {
		if in.AllDay {
			in.End = in.Start.AddDate(0, 0, 1)
		} else {
			in.End = in.Start.Add(time.Hour)
		}
	}
	_, err := db.sql.Exec(`UPDATE events SET calendar_id = ?, title = ?, description = ?, location = ?,
		starts_at = ?, ends_at = ?, all_day = ?, rrule = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`,
		in.CalendarID, strings.TrimSpace(in.Title), in.Description, in.Location,
		formatStamp(in.Start, in.AllDay), formatStamp(in.End, in.AllDay), boolToInt(in.AllDay),
		in.RRule, now(), userID, id)
	if err != nil {
		return nil, err
	}
	return db.EventByID(userID, id)
}

// MoveEvent shifts an event to a new start date, keeping its time-of-day and
// duration. Used by calendar drag-and-drop.
func (db *DB) MoveEvent(userID, id int64, newDate string) error {
	e, err := db.EventByID(userID, id)
	if err != nil {
		return err
	}
	target, err := time.ParseInLocation(stampDate, newDate, time.Local)
	if err != nil {
		return err
	}
	start, end := e.Start(), e.End()
	dur := end.Sub(start)
	newStart := time.Date(target.Year(), target.Month(), target.Day(),
		start.Hour(), start.Minute(), 0, 0, time.Local)
	newEnd := newStart.Add(dur)
	_, err = db.sql.Exec(`UPDATE events SET starts_at = ?, ends_at = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		formatStamp(newStart, e.AllDay), formatStamp(newEnd, e.AllDay), now(), userID, id)
	return err
}

// DeleteEvent removes an event.
func (db *DB) DeleteEvent(userID, id int64) error {
	_, err := db.sql.Exec(`DELETE FROM events WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

// ImportICS upserts parsed events into calendarID, keyed by iCal UID. Returns
// the number of events written.
func (db *DB) ImportICS(userID, calendarID int64, cal *parse.ICSCalendar) (int, error) {
	tx, err := db.sql.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	n := 0
	for _, ev := range cal.Events {
		if ev.Start.IsZero() {
			continue
		}
		title := ev.Summary
		if title == "" {
			title = "(untitled event)"
		}
		start := formatStamp(ev.Start.In(time.Local), ev.AllDay)
		end := formatStamp(ev.End.In(time.Local), ev.AllDay)

		if ev.UID != "" {
			var existing int64
			err := tx.QueryRow(`SELECT id FROM events WHERE calendar_id = ? AND uid = ?`, calendarID, ev.UID).Scan(&existing)
			if err == nil {
				if _, err := tx.Exec(`UPDATE events SET title=?, description=?, location=?, starts_at=?, ends_at=?, all_day=?, rrule=?, updated_at=?
					WHERE id = ?`, title, ev.Description, ev.Location, start, end, boolToInt(ev.AllDay), ev.RRule, now(), existing); err != nil {
					return n, err
				}
				n++
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return n, err
			}
		}
		if _, err := tx.Exec(`INSERT INTO events
			(user_id, calendar_id, uid, title, description, location, starts_at, ends_at, all_day, rrule, source, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?, 'import', ?, ?)`,
			userID, calendarID, ev.UID, title, ev.Description, ev.Location, start, end, boolToInt(ev.AllDay), ev.RRule, now(), now()); err != nil {
			return n, err
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// OccurrencesInRange returns everything to draw between from (inclusive) and to
// (exclusive): stored events (recurrences expanded) from visible calendars, plus
// tasks by due date when the Tasks calendar is visible. When includeHidden is
// true, hidden calendars are included too (used for per-calendar management).
func (db *DB) OccurrencesInRange(userID int64, from, to time.Time, includeHidden bool) ([]Occurrence, error) {
	cals, err := db.Calendars(userID)
	if err != nil {
		return nil, err
	}
	calByID := map[int64]Calendar{}
	var tasksCal *Calendar
	for i := range cals {
		calByID[cals[i].ID] = cals[i]
		if cals[i].Kind == "tasks" {
			c := cals[i]
			tasksCal = &c
		}
	}

	var out []Occurrence

	// Tasks by due date.
	if tasksCal != nil && (tasksCal.Visible || includeHidden) {
		rows, err := db.sql.Query(`SELECT slug, title, status, priority, due FROM entries
			WHERE user_id = ? AND due IS NOT NULL AND due >= ? AND due < ?`,
			userID, from.Format(stampDate), to.Format(stampDate))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var slug, title, status, due string
			var priority int
			if err := rows.Scan(&slug, &title, &status, &priority, &due); err != nil {
				rows.Close()
				return nil, err
			}
			d, perr := time.ParseInLocation(stampDate, due, time.Local)
			if perr != nil {
				continue
			}
			out = append(out, Occurrence{
				Kind: "task", Ref: slug, Title: title,
				Start: d, End: d.AddDate(0, 0, 1), AllDay: true,
				Done: status == "done", Priority: priority,
				Color: tasksCal.Color, CalendarID: tasksCal.ID, CalendarName: tasksCal.Name,
			})
		}
		rows.Close()
	}

	// Stored events.
	rows, err := db.sql.Query(`SELECT e.id, e.calendar_id, e.title, e.starts_at, e.ends_at, e.all_day, e.rrule
		FROM events e JOIN calendars c ON c.id = e.calendar_id
		WHERE e.user_id = ? AND (c.visible = 1 OR ?)`, userID, boolToInt(includeHidden))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, calID     int64
			title, ss, es string
			allDayInt     int
			rrule         string
		)
		if err := rows.Scan(&id, &calID, &title, &ss, &es, &allDayInt, &rrule); err != nil {
			return nil, err
		}
		cal, ok := calByID[calID]
		if !ok {
			continue
		}
		start, end := parseStamp(ss), parseStamp(es)
		if start.IsZero() {
			continue
		}
		dur := end.Sub(start)
		if dur <= 0 {
			if allDayInt != 0 {
				dur = 24 * time.Hour
			} else {
				dur = time.Hour
			}
		}
		starts := parse.ExpandRRule(start, rrule, from.AddDate(0, 0, -1), to, 500)
		for _, st := range starts {
			out = append(out, Occurrence{
				Kind: "event", Ref: itoa64(id), Title: title,
				Start: st, End: st.Add(dur), AllDay: allDayInt != 0,
				Color: cal.Color, CalendarID: cal.ID, CalendarName: cal.Name,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].Title < out[j].Title
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out, nil
}

func itoa64(n int64) string { return itoa(int(n)) }
