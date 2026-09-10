-- Server-side key/value settings (server secret lives here).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Session tokens are now stored hashed (sha256, hex). Existing plaintext rows
-- can't be migrated, so clear them — everyone re-logs in once.
DELETE FROM sessions;
