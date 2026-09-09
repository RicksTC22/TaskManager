CREATE TABLE calendars (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#3f6373',
    kind        TEXT NOT NULL DEFAULT 'events',   -- events | tasks | imported
    visible     INTEGER NOT NULL DEFAULT 1,
    is_default  INTEGER NOT NULL DEFAULT 0,       -- the built-in Tasks calendar
    source_name TEXT NOT NULL DEFAULT '',         -- original name of an imported .ics
    sort        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (user_id, slug)
);

CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    calendar_id INTEGER NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid         TEXT NOT NULL DEFAULT '',         -- iCal UID, for import de-dup
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    location    TEXT NOT NULL DEFAULT '',
    starts_at   TEXT NOT NULL,                    -- 'YYYY-MM-DDTHH:MM' local, or 'YYYY-MM-DD' when all_day
    ends_at     TEXT NOT NULL,
    all_day     INTEGER NOT NULL DEFAULT 0,
    rrule       TEXT NOT NULL DEFAULT '',         -- raw RRULE; expanded at read time
    source      TEXT NOT NULL DEFAULT 'manual',   -- manual | import
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX events_range_idx ON events(user_id, starts_at);
CREATE INDEX events_cal_idx   ON events(calendar_id);
CREATE UNIQUE INDEX events_uid_idx ON events(calendar_id, uid) WHERE uid <> '';
