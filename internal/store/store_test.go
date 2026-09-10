package store

import (
	"os"
	"path/filepath"
	"testing"
)

// openTest returns a store for each dialect available: always SQLite (a temp
// file), and Postgres when TEST_DATABASE_URL is set (CI provides one).
func eachDialect(t *testing.T, fn func(t *testing.T, db *DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.db")
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		fn(t, db)
	})

	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		t.Run("postgres", func(t *testing.T) {
			db, err := Open(url)
			if err != nil {
				t.Fatalf("open postgres: %v", err)
			}
			t.Cleanup(func() {
				// leave a clean slate for the next test
				db.sql.Exec(`TRUNCATE users, sessions, projects, entries, tags, entry_tags, links, calendars, events, settings RESTART IDENTITY CASCADE`)
				db.Close()
			})
			fn(t, db)
		})
	}
}

func TestUsersAndSessions(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *DB) {
		u, err := db.CreateUser("a@example.com", "", "supersecret")
		if err != nil {
			t.Fatal(err)
		}
		if u.ID == 0 || u.DisplayName != "a" {
			t.Fatalf("bad user: %+v", u)
		}
		if _, err := db.CreateUser("a@example.com", "", "another-one"); err != ErrEmailTaken {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
		if _, err := db.Authenticate("a@example.com", "wrong"); err != ErrBadCredentials {
			t.Fatalf("want ErrBadCredentials, got %v", err)
		}
		if _, err := db.Authenticate("a@example.com", "supersecret"); err != nil {
			t.Fatalf("auth should succeed: %v", err)
		}

		tok, err := db.CreateSession(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.UserBySession(tok)
		if err != nil || got.ID != u.ID {
			t.Fatalf("session lookup: %v %+v", err, got)
		}
		db.DeleteSession(tok)
		if _, err := db.UserBySession(tok); err != ErrNotFound {
			t.Fatalf("deleted session should be gone: %v", err)
		}
	})
}

func TestEntriesTagsLinksSearch(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *DB) {
		u, err := db.CreateUser("b@example.com", "B", "supersecret")
		if err != nil {
			t.Fatal(err)
		}

		note, err := db.CreateEntry(u.ID, EntryInput{
			Title: "Trip planning",
			Body:  "cabin near the lake #travel and see [[Pack list]]",
		})
		if err != nil {
			t.Fatal(err)
		}
		if note.Slug != "trip-planning" {
			t.Fatalf("slug: %q", note.Slug)
		}
		if len(note.Tags) != 1 || note.Tags[0] != "travel" {
			t.Fatalf("tags: %v", note.Tags)
		}

		// [[Pack list]] should have auto-created a stub that backlinks here.
		packSlug, ok := db.TitleSlug(u.ID, "Pack list")
		if !ok {
			t.Fatal("stub for Pack list not created")
		}
		pack, err := db.EntryBySlug(u.ID, packSlug)
		if err != nil {
			t.Fatal(err)
		}
		back, err := db.Backlinks(u.ID, pack.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(back) != 1 || back[0].ID != note.ID {
			t.Fatalf("backlinks: %+v", back)
		}

		// Full-text search across both dialects.
		hits, err := db.ListEntries(u.ID, EntryFilter{Query: "cabin"})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].ID != note.ID {
			t.Fatalf("search 'cabin': %+v", hits)
		}
		if h, _ := db.ListEntries(u.ID, EntryFilter{Query: "unrelatedword"}); len(h) != 0 {
			t.Fatalf("search miss should be empty: %+v", h)
		}

		// Updating the body re-syncs search + tags.
		if _, err := db.UpdateEntry(u.ID, note.ID, EntryInput{Title: "Trip planning", Body: "changed to mountains #hiking"}); err != nil {
			t.Fatal(err)
		}
		if h, _ := db.ListEntries(u.ID, EntryFilter{Query: "cabin"}); len(h) != 0 {
			t.Fatalf("stale search hit after update: %+v", h)
		}
		if h, _ := db.ListEntries(u.ID, EntryFilter{Query: "mountains"}); len(h) != 1 {
			t.Fatalf("new search term should hit: %+v", h)
		}
	})
}

func TestBoardAndDueFilters(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *DB) {
		u, _ := db.CreateUser("c@example.com", "C", "supersecret")

		mk := func(title, status, due string) *Entry {
			e, err := db.CreateEntry(u.ID, EntryInput{Title: title, Status: status, Due: due})
			if err != nil {
				t.Fatal(err)
			}
			return e
		}
		mk("overdue task", "next", dayFromNow(-3))
		mk("today task", "next", today())
		mk("future task", "inbox", dayFromNow(30))
		mk("a note", "", "")

		board, err := db.Board(u.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(board["next"]) != 2 || len(board["inbox"]) != 1 {
			t.Fatalf("board columns: next=%d inbox=%d", len(board["next"]), len(board["inbox"]))
		}

		over, _ := db.ListEntries(u.ID, EntryFilter{Due: "overdue"})
		if len(over) != 1 || over[0].Title != "overdue task" {
			t.Fatalf("overdue filter: %+v", over)
		}
		td, _ := db.ListEntries(u.ID, EntryFilter{Due: "today"})
		if len(td) != 1 || td[0].Title != "today task" {
			t.Fatalf("today filter: %+v", td)
		}
		notes, _ := db.ListEntries(u.ID, EntryFilter{OnlyNotes: true})
		if len(notes) != 1 || notes[0].Title != "a note" {
			t.Fatalf("notes filter: %+v", notes)
		}
	})
}

func TestProjectsCalendarsEvents(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *DB) {
		u, _ := db.CreateUser("d@example.com", "D", "supersecret")

		// upsertProject via CreateEntry (ON CONFLICT path), twice.
		if _, err := db.CreateEntry(u.ID, EntryInput{Title: "t1", Project: "Big Project"}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateEntry(u.ID, EntryInput{Title: "t2", Project: "Big Project"}); err != nil {
			t.Fatal(err)
		}
		projs, _ := db.Projects(u.ID)
		if len(projs) != 1 || projs[0].Count != 2 {
			t.Fatalf("projects: %+v", projs)
		}

		if err := db.EnsureDefaultCalendars(u.ID); err != nil {
			t.Fatal(err)
		}
		cals, _ := db.Calendars(u.ID)
		if len(cals) != 2 {
			t.Fatalf("default calendars: %+v", cals)
		}
		var eventsCal *Calendar
		for i := range cals {
			if cals[i].Kind == "events" {
				eventsCal = &cals[i]
			}
		}
		ev, err := db.CreateEvent(u.ID, EventInput{
			CalendarID: eventsCal.ID, Title: "Standup",
			Start: parseStamp("2026-09-10T09:00"), End: parseStamp("2026-09-10T09:15"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if ev.ID == 0 {
			t.Fatal("event id not returned")
		}
	})
}
