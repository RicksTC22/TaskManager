package web

import (
	"net/http"

	"taskmanager/internal/store"
)

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	q := r.URL.Query()

	view := q.Get("view") // "", "tasks", "notes"
	f := store.EntryFilter{
		Query:    q.Get("q"),
		Status:   q.Get("status"),
		Tag:      q.Get("tag"),
		Due:      q.Get("due"),
		Sort:     q.Get("sort"),
		TopLevel: true,
	}
	if f.Sort == "" {
		f.Sort = "updated"
	}
	switch view {
	case "tasks":
		f.OnlyTasks = true
	case "notes":
		f.OnlyNotes = true
	}
	var project *store.Project
	if ps := q.Get("project"); ps != "" {
		if p, err := s.db.ProjectBySlug(u.ID, ps); err == nil {
			project = p
			f.ProjectID = p.ID
		}
	}

	entries, err := s.db.ListEntries(u.ID, f)
	if err != nil {
		s.serverError(w, err)
		return
	}

	grouped := groupEntries(entries, f.Sort)
	layout := s.resolveListLayout(w, r)

	data := s.base(r, "List", "dense")
	data["Entries"] = entries
	data["Groups"] = grouped
	data["View"] = view
	data["Sort"] = f.Sort
	data["Status"] = f.Status
	data["Due"] = f.Due
	data["Query"] = f.Query
	data["Project"] = project
	data["Layout"] = layout
	data["Statuses"] = store.Statuses

	if htmxPartial(r) {
		s.pages.renderPartial(w, "list", "results", data)
		return
	}
	s.pages.render(w, http.StatusOK, "list", data)
}

// resolveListLayout picks "cards" (default) or "rows"; an explicit ?layout=
// param wins and is remembered in a cookie for later plain navigations.
func (s *Server) resolveListLayout(w http.ResponseWriter, r *http.Request) string {
	if l := r.URL.Query().Get("layout"); l == "cards" || l == "rows" {
		http.SetCookie(w, &http.Cookie{
			Name: "cairn_list", Value: l, Path: "/", SameSite: http.SameSiteLaxMode,
			Secure: s.isSecureRequest(r), MaxAge: 365 * 24 * 3600,
		})
		return l
	}
	if c, err := r.Cookie("cairn_list"); err == nil && (c.Value == "cards" || c.Value == "rows") {
		return c.Value
	}
	return "cards"
}

type entryGroup struct {
	Label   string
	Entries []*store.Entry
}

func groupEntries(entries []*store.Entry, sort string) []entryGroup {
	if sort == "due" {
		buckets := map[string][]*store.Entry{}
		order := []string{"Overdue", "Today", "This week", "Later", "No date"}
		for _, e := range entries {
			buckets[dueBucket(e.Due)] = append(buckets[dueBucket(e.Due)], e)
		}
		var out []entryGroup
		for _, k := range order {
			if len(buckets[k]) > 0 {
				out = append(out, entryGroup{Label: k, Entries: buckets[k]})
			}
		}
		return out
	}
	// Default: one group.
	return []entryGroup{{Label: "", Entries: entries}}
}

func dueBucket(iso string) string {
	switch dueClass(iso) {
	case "due-overdue":
		return "Overdue"
	case "due-today":
		return "Today"
	case "due-soon":
		return "This week"
	case "due-later":
		return "Later"
	default:
		return "No date"
	}
}
