package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Calendar is a named, colored, toggleable layer of events.
type Calendar struct {
	ID         int64
	Slug       string
	Name       string
	Color      string
	Kind       string // events | tasks | imported
	Visible    bool
	IsDefault  bool
	SourceName string
	Sort       int
	EventCount int
}

// Palette offered for new calendars, drawn from the app's mountain scheme.
var CalendarColors = []string{
	"#3f6373", "#b8863f", "#a5502c", "#5c7350", "#6a5a8c", "#417a86", "#8a5a3c", "#4a6572",
}

// Calendars lists the user's calendars in display order.
func (db *DB) Calendars(userID int64) ([]Calendar, error) {
	rows, err := db.qy(db.sql, `
		SELECT c.id, c.slug, c.name, c.color, c.kind, c.visible, c.is_default, c.source_name, c.sort,
		       (SELECT COUNT(*) FROM events e WHERE e.calendar_id = c.id)
		FROM calendars c WHERE c.user_id = ?
		ORDER BY c.is_default DESC, c.sort, `+db.ci("c.name"), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Calendar
	for rows.Next() {
		var c Calendar
		var visible, isDefault int
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Color, &c.Kind, &visible, &isDefault,
			&c.SourceName, &c.Sort, &c.EventCount); err != nil {
			return nil, err
		}
		c.Visible = visible != 0
		c.IsDefault = isDefault != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CalendarByID loads one calendar owned by the user.
func (db *DB) CalendarByID(userID, id int64) (*Calendar, error) {
	var c Calendar
	var visible, isDefault int
	err := db.row(db.sql, `SELECT id, slug, name, color, kind, visible, is_default, source_name, sort
		FROM calendars WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&c.ID, &c.Slug, &c.Name, &c.Color, &c.Kind, &visible, &isDefault, &c.SourceName, &c.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Visible = visible != 0
	c.IsDefault = isDefault != 0
	return &c, nil
}

// EnsureDefaultCalendars creates the built-in Tasks and Personal calendars for a
// user if they don't exist yet. Safe to call repeatedly.
func (db *DB) EnsureDefaultCalendars(userID int64) error {
	var n int
	if err := db.row(db.sql, `SELECT COUNT(*) FROM calendars WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := db.ex(db.sql, `INSERT INTO calendars (user_id, slug, name, color, kind, is_default, sort)
		VALUES (?, 'tasks', 'Tasks', ?, 'tasks', 1, 0)`, userID, "#b8863f"); err != nil {
		return err
	}
	_, err := db.ex(db.sql, `INSERT INTO calendars (user_id, slug, name, color, kind, sort)
		VALUES (?, 'personal', 'Personal', ?, 'events', 1)`, userID, "#3f6373")
	return err
}

// CreateCalendar makes a new calendar and returns it.
func (db *DB) CreateCalendar(userID int64, name, color, kind, sourceName string) (*Calendar, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Calendar"
	}
	if kind == "" {
		kind = "events"
	}
	if color == "" {
		color = CalendarColors[0]
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	slug, err := db.uniqueCalendarSlug(tx, userID, Slugify(name))
	if err != nil {
		return nil, err
	}
	var maxSort sql.NullInt64
	db.row(tx, `SELECT MAX(sort) FROM calendars WHERE user_id = ?`, userID).Scan(&maxSort)
	id, err := db.ins(tx, `INSERT INTO calendars (user_id, slug, name, color, kind, source_name, sort)
		VALUES (?,?,?,?,?,?,?)`, userID, slug, name, color, kind, sourceName, maxSort.Int64+1)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.CalendarByID(userID, id)
}

// UpdateCalendar changes a calendar's name and color.
func (db *DB) UpdateCalendar(userID, id int64, name, color string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	_, err := db.ex(db.sql, `UPDATE calendars SET name = ?, color = ? WHERE user_id = ? AND id = ?`,
		name, color, userID, id)
	return err
}

// SetCalendarVisible toggles a calendar's visibility on the composite view.
func (db *DB) SetCalendarVisible(userID, id int64, visible bool) error {
	_, err := db.ex(db.sql, `UPDATE calendars SET visible = ? WHERE user_id = ? AND id = ?`,
		boolToInt(visible), userID, id)
	return err
}

// DeleteCalendar removes a calendar (and its events); the default Tasks calendar
// cannot be deleted.
func (db *DB) DeleteCalendar(userID, id int64) error {
	var isDefault int
	err := db.row(db.sql, `SELECT is_default FROM calendars WHERE user_id = ? AND id = ?`, userID, id).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if isDefault != 0 {
		return errors.New("the Tasks calendar can't be deleted")
	}
	_, err = db.ex(db.sql, `DELETE FROM calendars WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

func (db *DB) uniqueCalendarSlug(tx *sql.Tx, userID int64, base string) (string, error) {
	if base == "" {
		base = "calendar"
	}
	slug := base
	for n := 2; ; n++ {
		var one int
		err := db.row(tx, `SELECT 1 FROM calendars WHERE user_id = ? AND slug = ?`, userID, slug).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = base + "-" + itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
