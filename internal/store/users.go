package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// ErrEmailTaken is returned when a signup collides with an existing account.
var ErrEmailTaken = errors.New("email already registered")

// ErrBadCredentials is returned for a failed login.
var ErrBadCredentials = errors.New("email or password is incorrect")

// User is an account.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	CreatedAt   string
}

// sessionTTL is how long a login lasts.
const sessionTTL = 30 * 24 * time.Hour

// CreateUser registers a new account and returns it.
func (db *DB) CreateUser(email, displayName, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 8 {
		return nil, errors.New("email required and password must be at least 8 characters")
	}
	if displayName == "" {
		displayName = strings.SplitN(email, "@", 2)[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := db.sql.Exec(`INSERT INTO users (email, display_name, password_hash) VALUES (?,?,?)`,
		email, displayName, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := db.EnsureDefaultCalendars(id); err != nil {
		return nil, err
	}
	return &User{ID: id, Email: email, DisplayName: displayName}, nil
}

// Authenticate checks a login and returns the user on success.
func (db *DB) Authenticate(email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		u    User
		hash string
	)
	err := db.sql.QueryRow(`SELECT id, email, display_name, password_hash, created_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &hash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrBadCredentials
	}
	return &u, nil
}

// CreateSession mints a session token for a user.
func (db *DB) CreateSession(userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().UTC().Add(sessionTTL).Format("2006-01-02 15:04:05")
	if _, err := db.sql.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?,?,?)`,
		hashToken(token), userID, expires); err != nil {
		return "", err
	}
	return token, nil
}

// UserBySession resolves a session token to its user, or ErrNotFound.
func (db *DB) UserBySession(token string) (*User, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var (
		u       User
		expires string
	)
	err := db.sql.QueryRow(`
		SELECT u.id, u.email, u.display_name, u.created_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, hashToken(token)).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if t, perr := time.Parse("2006-01-02 15:04:05", expires); perr == nil && time.Now().UTC().After(t) {
		db.DeleteSession(token)
		return nil, ErrNotFound
	}
	return &u, nil
}

// DeleteSession logs a session out. It accepts the raw token.
func (db *DB) DeleteSession(token string) error {
	_, err := db.sql.Exec(`DELETE FROM sessions WHERE token = ?`, hashToken(token))
	return err
}

// UserCount reports how many accounts exist (used to decide demo seeding).
func (db *DB) UserCount() (int, error) {
	var n int
	return n, db.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
}
