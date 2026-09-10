package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"taskmanager/internal/parse"
)

// Board column order.
var Statuses = []string{"inbox", "next", "doing", "done"}

// StatusLabels gives display names for the board columns.
var StatusLabels = map[string]string{
	"inbox": "Inbox", "next": "Next", "doing": "Doing", "done": "Done",
}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// Project groups entries.
type Project struct {
	ID    int64
	Slug  string
	Name  string
	Sort  int
	Count int
}

// Entry is the single content type: a note, or a task when Status is set.
type Entry struct {
	ID        int64
	Slug      string
	Title     string
	Body      string
	Status    string
	Priority  int
	Due       string
	ProjectID int64
	ParentID  int64
	BoardSort float64
	Pinned    bool
	CreatedAt string
	UpdatedAt string
	DoneAt    string

	Project  *Project
	Tags     []string
	Children []*Entry
}

// IsTask reports whether the entry sits on the board.
func (e *Entry) IsTask() bool { return e.Status != "" }

// EntryInput is the payload for creating or updating an entry.
type EntryInput struct {
	Title      string
	Body       string
	Status     string
	Priority   int
	Due        string
	Project    string // project name; "" leaves/clears
	ParentID   int64
	Pinned     bool
	ExtraTags  []string // unioned with #tags found in the body
	SetPinned  bool
	SetDue     bool
	SetProject bool
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts text into a URL-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugStrip.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// ci wraps a column for case-insensitive ordering in the active dialect.
func (db *DB) ci(expr string) string {
	if db.d == dialectPostgres {
		return "LOWER(" + expr + ")"
	}
	return expr + " COLLATE NOCASE"
}

// ---- Projects ---------------------------------------------------------------

// Projects lists a user's projects with entry counts.
func (db *DB) Projects(userID int64) ([]Project, error) {
	rows, err := db.qy(db.sql, `
		SELECT p.id, p.slug, p.name, p.sort,
		       (SELECT COUNT(*) FROM entries e WHERE e.project_id = p.id)
		FROM projects p WHERE p.user_id = ?
		ORDER BY p.sort, `+db.ci("p.name"), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Sort, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectBySlug looks up one project.
func (db *DB) ProjectBySlug(userID int64, slug string) (*Project, error) {
	var p Project
	err := db.row(db.sql, `SELECT id, slug, name, sort FROM projects WHERE user_id = ? AND slug = ?`,
		userID, slug).Scan(&p.ID, &p.Slug, &p.Name, &p.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (db *DB) upsertProject(tx *sql.Tx, userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	slug := Slugify(name)
	if slug == "" {
		return 0, nil
	}
	if _, err := db.ex(tx, `INSERT INTO projects (user_id, slug, name) VALUES (?,?,?)
		ON CONFLICT (user_id, slug) DO UPDATE SET name = excluded.name`, userID, slug, name); err != nil {
		return 0, err
	}
	var id int64
	return id, db.row(tx, `SELECT id FROM projects WHERE user_id = ? AND slug = ?`, userID, slug).Scan(&id)
}

// CreateProject makes an empty project.
func (db *DB) CreateProject(userID int64, name string) (*Project, error) {
	tx, err := db.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := db.upsertProject(tx, userID, name); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.ProjectBySlug(userID, Slugify(name))
}

// ---- Tags -----------------------------------------------------------------

// TagCount is a tag with how many entries use it.
type TagCount struct {
	Name  string
	Count int
}

// Tags lists a user's tags with counts, most-used first.
func (db *DB) Tags(userID int64) ([]TagCount, error) {
	rows, err := db.qy(db.sql, `
		SELECT t.name, COUNT(et.entry_id)
		FROM tags t LEFT JOIN entry_tags et ON et.tag_id = t.id
		WHERE t.user_id = ?
		GROUP BY t.id, t.name HAVING COUNT(et.entry_id) > 0
		ORDER BY COUNT(et.entry_id) DESC, t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Name, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- Entry reads ---------------------------------------------------------

func scanEntry(s interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var pinned int
	if err := s.Scan(&e.ID, &e.Slug, &e.Title, &e.Body, &e.Status, &e.Priority, &e.Due,
		&e.ProjectID, &e.ParentID, &e.BoardSort, &pinned, &e.CreatedAt, &e.UpdatedAt, &e.DoneAt); err != nil {
		return nil, err
	}
	e.Pinned = pinned != 0
	return &e, nil
}

// EntryBySlug loads one entry with its project, tags and direct children.
func (db *DB) EntryBySlug(userID int64, slug string) (*Entry, error) {
	row := db.row(db.sql, `SELECT `+colsFor("entries")+` FROM entries WHERE user_id = ? AND slug = ?`, userID, slug)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := db.hydrate(userID, e); err != nil {
		return nil, err
	}
	kids, err := db.childrenOf(userID, e.ID)
	if err != nil {
		return nil, err
	}
	e.Children = kids
	return e, nil
}

// EntryByID is EntryBySlug's id-keyed sibling (no children hydration).
func (db *DB) EntryByID(userID, id int64) (*Entry, error) {
	row := db.row(db.sql, `SELECT `+colsFor("entries")+` FROM entries WHERE user_id = ? AND id = ?`, userID, id)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := db.hydrate(userID, e); err != nil {
		return nil, err
	}
	return e, nil
}

func (db *DB) hydrate(userID int64, e *Entry) error {
	if e.ProjectID != 0 {
		var p Project
		if err := db.row(db.sql, `SELECT id, slug, name, sort FROM projects WHERE id = ?`, e.ProjectID).
			Scan(&p.ID, &p.Slug, &p.Name, &p.Sort); err == nil {
			e.Project = &p
		}
	}
	rows, err := db.qy(db.sql, `SELECT t.name FROM entry_tags et JOIN tags t ON t.id = et.tag_id
		WHERE et.entry_id = ? ORDER BY t.name`, e.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		e.Tags = append(e.Tags, n)
	}
	return rows.Err()
}

func (db *DB) childrenOf(userID, parentID int64) ([]*Entry, error) {
	rows, err := db.qy(db.sql, `SELECT `+colsFor("entries")+` FROM entries
		WHERE user_id = ? AND parent_id = ? ORDER BY (status='done'), board_sort, id`, userID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TitleSlug resolves an entry title (case-insensitive) to its slug.
func (db *DB) TitleSlug(userID int64, title string) (string, bool) {
	var slug string
	err := db.row(db.sql, `SELECT slug FROM entries WHERE user_id = ? AND lower(title) = lower(?)
		ORDER BY id LIMIT 1`, userID, strings.TrimSpace(title)).Scan(&slug)
	if err != nil {
		return "", false
	}
	return slug, true
}

// Backlinks returns entries whose body links to the given entry.
func (db *DB) Backlinks(userID, entryID int64) ([]*Entry, error) {
	rows, err := db.qy(db.sql, `SELECT `+colsFor("e")+`
		FROM links l JOIN entries e ON e.id = l.src_id
		WHERE l.dst_id = ? AND e.user_id = ?
		ORDER BY e.updated_at DESC`, entryID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EntryFilter narrows a list query.
type EntryFilter struct {
	Query     string
	Status    string // one status, or "" for any
	OnlyNotes bool
	OnlyTasks bool
	ProjectID int64
	Tag       string
	Due       string // "overdue" | "today" | "week" | "none"
	Sort      string // "updated" | "created" | "due" | "priority" | "title"
	TopLevel  bool   // exclude entries that have a parent
	Limit     int
}

// searchFilter returns the JOIN fragment, WHERE fragment and args that apply a
// full-text search for q in the active dialect.
func (db *DB) searchFilter(q string) (join, where string, args []any) {
	if db.d == dialectPostgres {
		return "", "e.search @@ websearch_to_tsquery('english', ?)", []any{q}
	}
	return " JOIN entry_fts fts ON fts.rowid = e.id ", "entry_fts MATCH ?", []any{ftsMatch(q)}
}

// ListEntries runs a filtered entry query.
func (db *DB) ListEntries(userID int64, f EntryFilter) ([]*Entry, error) {
	where := []string{"e.user_id = ?"}
	join := ""
	// Positional args must follow the SQL text order: JOIN clauses first, then
	// WHERE clauses.
	var joinArgs, whereArgs []any
	whereArgs = append(whereArgs, userID)

	if f.Tag != "" {
		join += " JOIN entry_tags xt ON xt.entry_id = e.id JOIN tags xtg ON xtg.id = xt.tag_id AND xtg.name = ? "
		joinArgs = append(joinArgs, strings.ToLower(f.Tag))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		sj, sw, sa := db.searchFilter(q)
		join += sj
		where = append(where, sw)
		whereArgs = append(whereArgs, sa...)
	}
	if f.Status != "" {
		where = append(where, "e.status = ?")
		whereArgs = append(whereArgs, f.Status)
	}
	if f.OnlyTasks {
		where = append(where, "e.status <> ''")
	}
	if f.OnlyNotes {
		where = append(where, "e.status = ''")
	}
	if f.ProjectID != 0 {
		where = append(where, "e.project_id = ?")
		whereArgs = append(whereArgs, f.ProjectID)
	}
	if f.TopLevel {
		where = append(where, "e.parent_id IS NULL")
	}
	switch f.Due {
	case "overdue":
		where = append(where, "e.due IS NOT NULL AND e.due < ? AND e.status <> 'done'")
		whereArgs = append(whereArgs, today())
	case "today":
		where = append(where, "e.due = ?")
		whereArgs = append(whereArgs, today())
	case "week":
		where = append(where, "e.due IS NOT NULL AND e.due <= ?")
		whereArgs = append(whereArgs, dayFromNow(7))
	case "none":
		where = append(where, "e.due IS NULL")
	}

	order := "e.updated_at DESC"
	switch f.Sort {
	case "created":
		order = "e.created_at DESC"
	case "due":
		order = "e.due IS NULL, e.due ASC, e.priority DESC"
	case "priority":
		order = "e.priority DESC, e.due IS NULL, e.due ASC"
	case "title":
		order = db.ci("e.title") + " ASC"
	case "board":
		order = "e.board_sort ASC, e.id ASC"
	}

	limit := ""
	if f.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	query := `SELECT ` + colsFor("e") + ` FROM entries e ` + join +
		` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY e.pinned DESC, ` + order + limit

	args := append(joinArgs, whereArgs...)
	rows, err := db.qy(db.sql, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return db.attachTags(out)
}

func colsFor(a string) string {
	return fmt.Sprintf(`%[1]s.id, %[1]s.slug, %[1]s.title, %[1]s.body, %[1]s.status, %[1]s.priority,
		COALESCE(%[1]s.due,''), COALESCE(%[1]s.project_id,0), COALESCE(%[1]s.parent_id,0),
		%[1]s.board_sort, %[1]s.pinned, %[1]s.created_at, %[1]s.updated_at, COALESCE(%[1]s.done_at,'')`, a)
}

func (db *DB) attachTags(entries []*Entry) ([]*Entry, error) {
	for _, e := range entries {
		rows, err := db.qy(db.sql, `SELECT t.name FROM entry_tags et JOIN tags t ON t.id = et.tag_id
			WHERE et.entry_id = ? ORDER BY t.name`, e.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return nil, err
			}
			e.Tags = append(e.Tags, n)
		}
		rows.Close()
	}
	return entries, nil
}

// Board returns the four columns, each ordered for display, plus done limited.
func (db *DB) Board(userID int64, projectID int64) (map[string][]*Entry, error) {
	out := map[string][]*Entry{}
	for _, st := range Statuses {
		f := EntryFilter{Status: st, Sort: "board", ProjectID: projectID}
		if st == "done" {
			f.Sort = "updated"
			f.Limit = 30
		}
		list, err := db.ListEntries(userID, f)
		if err != nil {
			return nil, err
		}
		out[st] = list
	}
	return out, nil
}

// ---- Entry writes ------------------------------------------------------

// CreateEntry inserts a new entry and its tags/links/FTS row.
func (db *DB) CreateEntry(userID int64, in EntryInput) (*Entry, error) {
	if in.Status != "" && !validStatus(in.Status) {
		return nil, fmt.Errorf("invalid status %q", in.Status)
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	title := strings.TrimSpace(in.Title)

	// Adopt an empty stub that a [[wiki-link]] created earlier for this title,
	// rather than inserting a near-duplicate under a "-2" slug.
	if title != "" {
		var stubSlug string
		err := db.row(tx, `SELECT slug FROM entries
			WHERE user_id = ? AND lower(title) = lower(?) AND body = '' AND status = '' AND parent_id IS NULL
			ORDER BY id LIMIT 1`, userID, title).Scan(&stubSlug)
		if err == nil {
			tx.Rollback() // nothing written; hand off to UpdateEntry
			stub, gerr := db.EntryBySlug(userID, stubSlug)
			if gerr != nil {
				return nil, gerr
			}
			return db.UpdateEntry(userID, stub.ID, in)
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}

	base := Slugify(title)
	if base == "" {
		base = Slugify(firstWords(in.Body, 8))
	}
	if base == "" {
		base = "note"
	}
	slug, err := db.uniqueSlug(tx, userID, base)
	if err != nil {
		return nil, err
	}

	var projectID any
	if pid, err := db.upsertProject(tx, userID, in.Project); err != nil {
		return nil, err
	} else if pid != 0 {
		projectID = pid
	}
	var parentID any
	if in.ParentID != 0 {
		parentID = in.ParentID
	}
	var due any
	if in.Due != "" {
		due = in.Due
	}
	var doneAt any
	if in.Status == "done" {
		doneAt = now()
	}
	nextSort, _ := db.columnTail(tx, userID, in.Status)

	id, err := db.ins(tx, `INSERT INTO entries
		(user_id, slug, title, body, status, priority, due, project_id, parent_id, board_sort, pinned, created_at, updated_at, done_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, slug, title, in.Body, in.Status, clampPriority(in.Priority), due, projectID, parentID,
		nextSort, boolToInt(in.Pinned), now(), now(), doneAt)
	if err != nil {
		return nil, err
	}

	if err := db.syncTags(tx, userID, id, unionTags(in.Body, in.ExtraTags)); err != nil {
		return nil, err
	}
	if err := db.syncLinks(tx, userID, id, in.Body); err != nil {
		return nil, err
	}
	if err := db.syncFTS(tx, id, title, in.Body); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.EntryBySlug(userID, slug)
}

// UpdateEntry applies in to an existing entry. Only the Set* / non-zero fields
// that the caller populated are written; Title/Body/Status/Priority always are.
func (db *DB) UpdateEntry(userID, id int64, in EntryInput) (*Entry, error) {
	if in.Status != "" && !validStatus(in.Status) {
		return nil, fmt.Errorf("invalid status %q", in.Status)
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var cur Entry
	err = db.row(tx, `SELECT slug, status FROM entries WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&cur.Slug, &cur.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(in.Title)
	sets := []string{"title = ?", "body = ?", "status = ?", "priority = ?", "updated_at = ?"}
	args := []any{title, in.Body, in.Status, clampPriority(in.Priority), now()}

	if in.Status == "done" && cur.Status != "done" {
		sets = append(sets, "done_at = ?")
		args = append(args, now())
	} else if in.Status != "done" && cur.Status == "done" {
		sets = append(sets, "done_at = NULL")
	}
	if in.Status != cur.Status && in.Status != "" {
		if tail, err := db.columnTail(tx, userID, in.Status); err == nil {
			sets = append(sets, "board_sort = ?")
			args = append(args, tail)
		}
	}
	if in.SetDue {
		if in.Due == "" {
			sets = append(sets, "due = NULL")
		} else {
			sets = append(sets, "due = ?")
			args = append(args, in.Due)
		}
	}
	if in.SetPinned {
		sets = append(sets, "pinned = ?")
		args = append(args, boolToInt(in.Pinned))
	}
	if in.SetProject {
		if in.Project == "" {
			sets = append(sets, "project_id = NULL")
		} else {
			pid, err := db.upsertProject(tx, userID, in.Project)
			if err != nil {
				return nil, err
			}
			sets = append(sets, "project_id = ?")
			args = append(args, pid)
		}
	}

	args = append(args, userID, id)
	if _, err := db.ex(tx, `UPDATE entries SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id = ?`, args...); err != nil {
		return nil, err
	}
	if err := db.syncTags(tx, userID, id, unionTags(in.Body, in.ExtraTags)); err != nil {
		return nil, err
	}
	if err := db.syncLinks(tx, userID, id, in.Body); err != nil {
		return nil, err
	}
	if err := db.syncFTS(tx, id, title, in.Body); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.EntryBySlug(userID, cur.Slug)
}

// MoveEntry sets an entry's board status and position (before beforeID, or end
// of column when beforeID is 0).
func (db *DB) MoveEntry(userID, id int64, status string, beforeID int64) error {
	if !validStatus(status) {
		return fmt.Errorf("invalid status %q", status)
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sortVal float64
	if beforeID != 0 {
		var before float64
		if err := db.row(tx, `SELECT board_sort FROM entries WHERE user_id = ? AND id = ? AND status = ?`,
			userID, beforeID, status).Scan(&before); err == nil {
			var prev sql.NullFloat64
			db.row(tx, `SELECT MAX(board_sort) FROM entries WHERE user_id = ? AND status = ? AND board_sort < ?`,
				userID, status, before).Scan(&prev)
			if prev.Valid {
				sortVal = (prev.Float64 + before) / 2
			} else {
				sortVal = before - 1
			}
		} else {
			sortVal, _ = db.columnTail(tx, userID, status)
		}
	} else {
		sortVal, _ = db.columnTail(tx, userID, status)
	}

	var curStatus string
	if err := db.row(tx, `SELECT status FROM entries WHERE user_id = ? AND id = ?`, userID, id).Scan(&curStatus); err != nil {
		return err
	}
	sets := "status = ?, board_sort = ?, updated_at = ?"
	args := []any{status, sortVal, now()}
	if status == "done" && curStatus != "done" {
		sets += ", done_at = ?"
		args = append(args, now())
	} else if status != "done" && curStatus == "done" {
		sets += ", done_at = NULL"
	}
	args = append(args, userID, id)
	if _, err := db.ex(tx, `UPDATE entries SET `+sets+` WHERE user_id = ? AND id = ?`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// ToggleDone flips an entry between 'done' and 'inbox' (or 'next' if it has a
// due date). Returns the new status.
func (db *DB) ToggleDone(userID, id int64) (string, error) {
	var status, due string
	err := db.row(db.sql, `SELECT status, COALESCE(due,'') FROM entries WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&status, &due)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	target := "done"
	if status == "done" {
		target = "inbox"
		if due != "" {
			target = "next"
		}
	} else if status == "" {
		// a pure note being checked becomes a done task
		target = "done"
	}
	return target, db.MoveEntry(userID, id, target, 0)
}

// SetPinned toggles the pin flag.
func (db *DB) SetPinned(userID, id int64, pinned bool) error {
	_, err := db.ex(db.sql, `UPDATE entries SET pinned = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		boolToInt(pinned), now(), userID, id)
	return err
}

// DeleteEntry removes an entry (children cascade).
func (db *DB) DeleteEntry(userID, id int64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kids []int64
	rows, _ := db.qy(tx, `SELECT id FROM entries WHERE user_id = ? AND (id = ? OR parent_id = ?)`, userID, id, id)
	for rows.Next() {
		var k int64
		rows.Scan(&k)
		kids = append(kids, k)
	}
	rows.Close()
	if _, err := db.ex(tx, `DELETE FROM entries WHERE user_id = ? AND id = ?`, userID, id); err != nil {
		return err
	}
	if db.d == dialectSQLite {
		for _, k := range kids {
			db.ex(tx, `DELETE FROM entry_fts WHERE rowid = ?`, k)
		}
	}
	return tx.Commit()
}

// ---- helpers ----------------------------------------------------------

func (db *DB) syncTags(tx *sql.Tx, userID, entryID int64, names []string) error {
	if _, err := db.ex(tx, `DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := db.ex(tx, `INSERT INTO tags (user_id, name) VALUES (?,?)
			ON CONFLICT (user_id, name) DO NOTHING`, userID, name); err != nil {
			return err
		}
		var tagID int64
		if err := db.row(tx, `SELECT id FROM tags WHERE user_id = ? AND name = ?`, userID, name).Scan(&tagID); err != nil {
			return err
		}
		if _, err := db.ex(tx, db.orIgnore(`INSERT OR IGNORE INTO entry_tags (entry_id, tag_id) VALUES (?,?)`), entryID, tagID); err != nil {
			return err
		}
	}
	return nil
}

// syncLinks rebuilds outgoing [[wiki-link]] edges for an entry, creating stub
// entries for targets that don't exist yet.
func (db *DB) syncLinks(tx *sql.Tx, userID, entryID int64, body string) error {
	if _, err := db.ex(tx, `DELETE FROM links WHERE src_id = ?`, entryID); err != nil {
		return err
	}
	for _, target := range parse.Links(body) {
		dstID, err := db.resolveOrStub(tx, userID, target)
		if err != nil {
			return err
		}
		if dstID == entryID || dstID == 0 {
			continue
		}
		if _, err := db.ex(tx, db.orIgnore(`INSERT OR IGNORE INTO links (src_id, dst_id) VALUES (?,?)`), entryID, dstID); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) resolveOrStub(tx *sql.Tx, userID int64, title string) (int64, error) {
	title = strings.TrimSpace(title)
	var id int64
	err := db.row(tx, `SELECT id FROM entries WHERE user_id = ? AND lower(title) = lower(?)
		ORDER BY id LIMIT 1`, userID, title).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	slug, err := db.uniqueSlug(tx, userID, Slugify(title))
	if err != nil {
		return 0, err
	}
	newID, err := db.ins(tx, `INSERT INTO entries (user_id, slug, title, body, status, created_at, updated_at)
		VALUES (?,?,?,?,'',?,?)`, userID, slug, title, "", now(), now())
	if err != nil {
		return 0, err
	}
	db.syncFTS(tx, newID, title, "")
	return newID, nil
}

func (db *DB) syncFTS(tx *sql.Tx, entryID int64, title, body string) error {
	if db.d == dialectPostgres {
		_, err := db.ex(tx, `UPDATE entries
			SET search = to_tsvector('english', coalesce(?,'') || ' ' || coalesce(?,''))
			WHERE id = ?`, title, body, entryID)
		return err
	}
	if _, err := db.ex(tx, `DELETE FROM entry_fts WHERE rowid = ?`, entryID); err != nil {
		return err
	}
	_, err := db.ex(tx, `INSERT INTO entry_fts (rowid, title, body) VALUES (?,?,?)`, entryID, title, body)
	return err
}

func (db *DB) uniqueSlug(tx *sql.Tx, userID int64, base string) (string, error) {
	if base == "" {
		base = "note"
	}
	slug := base
	for n := 2; ; n++ {
		var one int
		err := db.row(tx, `SELECT 1 FROM entries WHERE user_id = ? AND slug = ?`, userID, slug).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}

func (db *DB) columnTail(tx *sql.Tx, userID int64, status string) (float64, error) {
	var v sql.NullFloat64
	err := db.row(tx, `SELECT MAX(board_sort) FROM entries WHERE user_id = ? AND status = ?`, userID, status).Scan(&v)
	if err != nil {
		return 0, err
	}
	if v.Valid {
		return v.Float64 + 1, nil
	}
	return 0, nil
}

func unionTags(body string, extra []string) []string {
	set := map[string]bool{}
	var out []string
	add := func(t string) {
		t = parse.NormalizeTag(t)
		if t != "" && !set[t] {
			set[t] = true
			out = append(out, t)
		}
	}
	for _, t := range parse.Tags(body) {
		add(t)
	}
	for _, t := range extra {
		add(t)
	}
	return out
}

func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}

func clampPriority(p int) int {
	if p < 0 {
		return 0
	}
	if p > 3 {
		return 3
	}
	return p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ftsMatch turns raw user input into a safe SQLite FTS5 prefix query.
func ftsMatch(q string) string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	for i, f := range fields {
		fields[i] = `"` + f + `"*`
	}
	if len(fields) == 0 {
		return `""`
	}
	return strings.Join(fields, " AND ")
}
