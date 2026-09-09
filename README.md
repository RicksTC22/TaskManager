# Cairn

A notebook and a task board in one, built with **Go + htmx + SQLite** — the same
stack and layout as the Cookbook project.

Every item is a single content type, an **entry**: a title plus a light-markdown
body. Give an entry a status and it appears on the board; leave the status off and
it is just a note. Either kind can hold prose, subtasks, `#tags` and
`[[wiki-links]]`.

## Features

- **Accounts** — email + password (bcrypt), server-side sessions (tokens stored
  hashed), per-user data isolation. CSRF protection on every mutating request,
  security headers + CSP, and cookies auto-marked `Secure` behind HTTPS
  (`-secure-cookies` / `CAIRN_SECURE_COOKIES=1` to force). A server secret is
  generated and stored on first run (`-secret` / `CAIRN_SECRET` to supply one).
- **Board** — Inbox / Next / Doing / Done, drag cards between columns.
- **List** — a grid of cards (with a **Cards / Rows** toggle, remembered);
  filter by kind, status, due window, tag or project; sort; group by due date.
- **Calendar** — month / week / day views.
  - Tasks appear automatically on their due date (the built-in **Tasks**
    calendar); drag one to another day to reschedule it.
  - First-class **events** with time, location, notes and recurrence; drag to
    move, click to edit.
  - **Multiple calendars**, each with a color; one composite grid with a
    per-calendar **visibility toggle** in the sidebar.
  - **Import `.ics` files** — a new calendar per file by default (named from the
    file), or add into an existing one. Re-importing updates events by iCal UID.
    `RRULE` (FREQ=DAILY/WEEKLY/MONTHLY/YEARLY with INTERVAL/COUNT/UNTIL/BYDAY) is
    expanded for display within the visible range.
  - Click a day in month view to add an event; "+ New event" for the full form.
- **Idea dump** — paste raw lines, Cairn splits them into tasks and notes and
  pulls out structure (see below). Nothing saves until you review it.
- **Wiki-links** — `[[Title]]` links entries; targets that don't exist yet are
  created as stubs. Each entry shows its **backlinks** ("Linked from").
- **Light markdown** in bodies: headings, lists, `- [ ]` checkboxes, `**bold**`,
  `*italic*`, `` `code` ``, fenced code, quotes, links, autolinked URLs,
  `#tags`, `[[wiki-links]]`. All input is escaped before rendering.
- **Subtasks** — any entry can be a parent; children show on its page with
  progress on the board card.
- **Two display modes** — a header toggle (persisted per browser):
  - **Minimal** — serif, wide margins, few lines. For the dump and the editor.
  - **Dense** — compact rows and cards. For the board and list.
- **Priorities** (low / medium / high), **due dates** with overdue/today/soon
  styling, **projects**, **tags** — all filterable.
- **Full-text search** over titles and bodies (SQLite FTS5).

### Idea-dump parsing (rule-based, offline)

A **blank line** separates blocks. Within a block, the first line is the item; a
line starting with `-` (or `*`, `+`, `- [ ]`, `1.`) becomes one of its
**subtasks**, and any other line is folded into the item's body. A block that
opens with a bullet has no head line, so each bullet is a top-level item instead.
An item that gets subtasks is treated as a task.

| In the text | Becomes |
|---|---|
| `#tag` | a tag |
| `[[Title]]` | a link |
| `@name` or `+name` | the project |
| `!high` / `!2` / `!p3` | priority |
| `due tomorrow`, `by fri`, `~sep 10`, or a bare `tomorrow` / `9/10` / `mon` | due date |
| leading action verb (`call`, `email`, `buy`, `fix`, …) or `TODO:` on a head line | makes it a **task** (status Inbox) |
| `- [x]` | a task already marked done |

Everything else is a **note**. Consumed tokens (`!`, `@`, date words) are
stripped from the visible title; `#tags` and `[[links]]` stay inline.

## Run it

```bash
go run .
```

Then open <http://localhost:8080>. On first run a demo account is created:

```
email:    demo@cairn.local
password: demodemo
```

The database is created at `data/cairn.db`.

### Configuration

| Flag / env | Default | Purpose |
|---|---|---|
| `-addr` / `CAIRN_ADDR` (or `PORT`) | `:8080` | listen address |
| `-db` / `CAIRN_DB` | `data/cairn.db` | SQLite file path |
| `-site-name` / `CAIRN_SITE_NAME` | `Cairn` | name shown in the UI |
| `-demo` | `true` | create the demo account when the database is empty |
| `-secret` / `CAIRN_SECRET` | generated | server secret (persisted to the DB) |
| `-secure-cookies` / `CAIRN_SECURE_COOKIES` | auto | force the `Secure` cookie flag |

Times are stored and shown in the server's local timezone; `.ics` imports are
converted to it (honouring `TZID` when the zone database can resolve it).

### Test

```bash
go test ./...
```

## Layout

```
main.go                     entrypoint, flags, graceful shutdown
internal/parse/             pure text logic — no DB
  tokens.go                   #tag and [[link]] extraction
  dates.go                    natural-language date parsing
  dump.go                     idea-dump line parser
  render.go                   light-markdown -> safe HTML
  ics.go / rrule.go           iCalendar parsing + recurrence expansion
internal/store/             SQLite persistence
  migrations/*.sql            schema, applied on startup
  store.go / settings.go / users.go / entries.go
  calendars.go / events.go / seed.go
internal/web/               HTTP: routing, handlers, templates, static assets
  security.go                 CSRF, security headers, HTTPS detection
  calendar.go / calview.go    calendar handlers + view assembly
  templates/                  layout + partials + pages (html/template)
  static/                     app.css, app.js, vendored htmx
```

## Deployment (target)

Single static binary — templates, migrations and assets are embedded.

```bash
CGO_ENABLED=0 go build -o cairn .
```

Run behind a reverse proxy that terminates TLS. Back up `data/cairn.db` (e.g.
Litestream). Sessions are cookies over `SameSite=Lax`; serve over HTTPS in
production and consider adding the `Secure` flag.
