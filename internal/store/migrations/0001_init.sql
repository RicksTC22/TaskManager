-- Accounts --------------------------------------------------------------

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions(user_id);

-- Content --------------------------------------------------------------

CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    sort       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (user_id, slug)
);

-- An entry is the single content type. It is a note by default; giving it a
-- status ('inbox' | 'next' | 'doing' | 'done') makes it show up on the board.
CREATE TABLE entries (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT '',      -- '' note | inbox | next | doing | done
    priority   INTEGER NOT NULL DEFAULT 0,    -- 0 none | 1 low | 2 medium | 3 high
    due        TEXT,                          -- 'YYYY-MM-DD' or NULL
    project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
    parent_id  INTEGER REFERENCES entries(id) ON DELETE CASCADE,
    board_sort REAL NOT NULL DEFAULT 0,
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    done_at    TEXT,
    UNIQUE (user_id, slug)
);
CREATE INDEX entries_board_idx  ON entries(user_id, status, board_sort);
CREATE INDEX entries_due_idx    ON entries(user_id, due);
CREATE INDEX entries_parent_idx ON entries(parent_id);
CREATE INDEX entries_project_idx ON entries(project_id);

CREATE TABLE tags (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    UNIQUE (user_id, name)
);

CREATE TABLE entry_tags (
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);
CREATE INDEX entry_tags_tag_idx ON entry_tags(tag_id);

-- Directed [[wiki-link]] edges between entries of the same user.
CREATE TABLE links (
    src_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    dst_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    PRIMARY KEY (src_id, dst_id)
);
CREATE INDEX links_dst_idx ON links(dst_id);

CREATE VIRTUAL TABLE entry_fts USING fts5(title, body);
