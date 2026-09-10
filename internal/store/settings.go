package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
)

// Setting reads a settings value, or "" if absent.
func (db *DB) Setting(key string) (string, error) {
	var v string
	err := db.row(db.sql, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting writes a settings value.
func (db *DB) SetSetting(key, value string) error {
	_, err := db.ex(db.sql, `INSERT INTO settings (key, value) VALUES (?,?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ServerSecret returns the persistent server secret, generating and storing a
// fresh 32-byte value the first time. If override is non-empty it is used and
// persisted instead (so an env-supplied secret wins and survives restarts).
func (db *DB) ServerSecret(override string) ([]byte, error) {
	if override != "" {
		if err := db.SetSetting("secret", override); err != nil {
			return nil, err
		}
		return []byte(override), nil
	}
	cur, err := db.Setting("secret")
	if err != nil {
		return nil, err
	}
	if cur != "" {
		return []byte(cur), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	val := base64.RawURLEncoding.EncodeToString(raw)
	if err := db.SetSetting("secret", val); err != nil {
		return nil, err
	}
	return []byte(val), nil
}
