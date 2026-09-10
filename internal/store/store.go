// Package store is the persistence layer for Cairn. It targets SQLite by
// default and PostgreSQL when the DSN is a postgres:// URL; the SQL is written
// once with `?` placeholders and rebound per dialect.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // "pgx" driver
	_ "modernc.org/sqlite"             // "sqlite" driver
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

func (d dialect) name() string {
	if d == dialectPostgres {
		return "postgres"
	}
	return "sqlite"
}

// DB wraps the database connection pool and remembers which dialect it speaks.
type DB struct {
	sql *sql.DB
	d   dialect
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// IsPostgresDSN reports whether dsn should be opened as PostgreSQL.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// Open opens the database at dsn (a file path for SQLite, or a postgres:// URL)
// and applies any pending migrations.
func Open(dsn string) (*DB, error) {
	if IsPostgresDSN(dsn) {
		return openPostgres(dsn)
	}
	return openSQLite(dsn)
}

func openSQLite(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1) // SQLite: single writer keeps things simple and correct
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB, d: dialectSQLite}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func openPostgres(dsn string) (*DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB, d: dialectPostgres}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close closes the underlying pool.
func (db *DB) Close() error { return db.sql.Close() }

// SQL exposes the raw handle for the rare caller that needs it (e.g. seeding).
func (db *DB) SQL() *sql.DB { return db.sql }

// Dialect reports the SQL dialect in use ("sqlite" or "postgres").
func (db *DB) Dialect() string { return db.d.name() }

// rebind turns `?` placeholders into `$1, $2, …` for PostgreSQL. SQLite gets the
// query unchanged. (Query strings never contain a literal `?`.)
func (db *DB) rebind(q string) string {
	if db.d != dialectPostgres {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (db *DB) ex(q execer, query string, args ...any) (sql.Result, error) {
	return q.Exec(db.rebind(query), args...)
}

func (db *DB) qy(q execer, query string, args ...any) (*sql.Rows, error) {
	return q.Query(db.rebind(query), args...)
}

func (db *DB) row(q execer, query string, args ...any) *sql.Row {
	return q.QueryRow(db.rebind(query), args...)
}

// ins runs an INSERT and returns the new row's id. The query must be a single
// INSERT whose target table has an `id` column; Postgres gets `RETURNING id`
// appended, SQLite uses LastInsertId.
func (db *DB) ins(q execer, query string, args ...any) (int64, error) {
	if db.d == dialectPostgres {
		var id int64
		err := q.QueryRow(db.rebind(query)+" RETURNING id", args...).Scan(&id)
		return id, err
	}
	res, err := q.Exec(db.rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// orIgnore adapts SQLite's "INSERT OR IGNORE" to each dialect.
func (db *DB) orIgnore(q string) string {
	if db.d == dialectPostgres {
		return strings.Replace(q, "INSERT OR IGNORE INTO", "INSERT INTO", 1) + " ON CONFLICT DO NOTHING"
	}
	return q
}

func (db *DB) migrate() error {
	if _, err := db.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	dir := "migrations/" + db.d.name()
	entries, err := migrationFS.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var seen string
		err := db.row(db.sql, `SELECT name FROM schema_migrations WHERE name = ?`, name).Scan(&seen)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return err
		}
		body, err := migrationFS.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		tx, err := db.sql.Begin()
		if err != nil {
			return err
		}
		// pgx's extended protocol rejects multi-statement Exec, so run each
		// statement on its own. The migration files contain no ';' inside
		// string literals or function bodies, so a plain split is safe.
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if _, err := db.ex(tx, `INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`, name, now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements breaks a migration file into individual statements on `;`,
// ignoring semicolons inside `--` line comments and `'…'` string literals, and
// dropping fragments that are only comments or whitespace.
func splitStatements(body string) []string {
	var out []string
	var cur strings.Builder
	inComment, inString := false, false
	rs := []rune(body)

	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s == "" || commentOnly(s) {
			return
		}
		out = append(out, s)
	}

	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case inComment:
			cur.WriteRune(c)
			if c == '\n' {
				inComment = false
			}
		case inString:
			cur.WriteRune(c)
			if c == '\'' {
				if i+1 < len(rs) && rs[i+1] == '\'' { // '' escape
					cur.WriteRune(rs[i+1])
					i++
				} else {
					inString = false
				}
			}
		case c == '-' && i+1 < len(rs) && rs[i+1] == '-':
			inComment = true
			cur.WriteRune(c)
		case c == '\'':
			inString = true
			cur.WriteRune(c)
		case c == ';':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	return out
}

func commentOnly(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" && !strings.HasPrefix(t, "--") {
			return false
		}
	}
	return true
}

func now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }

// today returns the current local date as YYYY-MM-DD (replaces SQLite's
// date('now','localtime') in due-date filters so the SQL stays dialect-neutral).
func today() string { return time.Now().Format("2006-01-02") }

func dayFromNow(days int) string { return time.Now().AddDate(0, 0, days).Format("2006-01-02") }
