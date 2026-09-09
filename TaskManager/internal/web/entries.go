package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"taskmanager/internal/store"
)

// lookupEntry loads the {slug} entry for the current user, writing a 404/500
// response and returning nil on failure.
func (s *Server) lookupEntry(w http.ResponseWriter, r *http.Request) *store.Entry {
	u := currentUser(r)
	e, err := s.db.EntryBySlug(u.ID, r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return nil
	}
	if err != nil {
		s.serverError(w, err)
		return nil
	}
	return e
}

func entryInputFromForm(r *http.Request) store.EntryInput {
	priority, _ := strconv.Atoi(r.FormValue("priority"))
	status := r.FormValue("status")
	in := store.EntryInput{
		Title:      strings.TrimSpace(r.FormValue("title")),
		Body:       r.FormValue("body"),
		Status:     status,
		Priority:   priority,
		SetDue:     true,
		Due:        strings.TrimSpace(r.FormValue("due")),
		SetProject: true,
		Project:    strings.TrimSpace(r.FormValue("project")),
		SetPinned:  true,
		Pinned:     r.FormValue("pinned") == "on" || r.FormValue("pinned") == "1",
	}
	if pid := r.FormValue("parent"); pid != "" {
		if n, err := strconv.ParseInt(pid, 10, 64); err == nil {
			in.ParentID = n
		}
	}
	return in
}

func (s *Server) handleEntryCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	in := entryInputFromForm(r)
	if in.Title == "" && strings.TrimSpace(in.Body) == "" {
		http.Redirect(w, r, "/board", http.StatusSeeOther)
		return
	}
	e, err := s.db.CreateEntry(u.ID, in)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// htmx quick-add from a board column: return the refreshed board.
	if r.Header.Get("HX-Request") == "true" && r.FormValue("from") == "board" {
		var projectID int64
		projectSlug := r.FormValue("project_slug")
		if projectSlug != "" {
			if p, perr := s.db.ProjectBySlug(u.ID, projectSlug); perr == nil {
				projectID = p.ID
			}
		}
		board, err := s.db.Board(u.ID, projectID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		data := s.base(r, "Board", "dense")
		data["Board"] = board
		data["Statuses"] = store.Statuses
		data["ProjectSlug"] = projectSlug
		s.pages.renderPartial(w, "board", "board", data)
		return
	}
	if r.Header.Get("HX-Request") == "true" && r.FormValue("from") == "entry" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/e/"+e.Slug, http.StatusSeeOther)
}

func (s *Server) handleEntry(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	backlinks, err := s.db.Backlinks(u.ID, e.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	var parent *store.Entry
	if e.ParentID != 0 {
		parent, _ = s.db.EntryByID(u.ID, e.ParentID)
	}

	mode := "minimal"
	data := s.base(r, e.Title, mode)
	data["E"] = e
	data["BodyHTML"] = s.bodyHTML(u.ID, e.Body)
	data["Backlinks"] = backlinks
	data["Parent"] = parent
	data["Editing"] = false
	data["Statuses"] = store.Statuses
	s.pages.render(w, http.StatusOK, "entry", data)
}

func (s *Server) handleEntryEdit(w http.ResponseWriter, r *http.Request) {
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	data := s.base(r, "Edit — "+e.Title, "minimal")
	data["E"] = e
	data["Editing"] = true
	data["Statuses"] = store.Statuses
	s.pages.render(w, http.StatusOK, "entry", data)
}

func (s *Server) handleEntrySave(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	in := entryInputFromForm(r)
	if in.Title == "" {
		in.Title = e.Title
	}
	updated, err := s.db.UpdateEntry(u.ID, e.ID, in)
	if err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/e/"+updated.Slug, http.StatusSeeOther)
}

func (s *Server) handleEntryToggle(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	if _, err := s.db.ToggleDone(u.ID, e.ID); err != nil {
		s.serverError(w, err)
		return
	}

	// From a board card: re-render the whole board.
	if r.FormValue("from") == "board" {
		var projectID int64
		projectSlug := r.FormValue("project")
		if projectSlug != "" {
			if p, perr := s.db.ProjectBySlug(u.ID, projectSlug); perr == nil {
				projectID = p.ID
			}
		}
		board, err := s.db.Board(u.ID, projectID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		data := s.base(r, "Board", "dense")
		data["Board"] = board
		data["Statuses"] = store.Statuses
		data["ProjectSlug"] = projectSlug
		s.pages.renderPartial(w, "board", "board", data)
		return
	}

	// From a list card or row: re-render just that item.
	if htmxPartial(r) {
		fresh, err := s.db.EntryBySlug(u.ID, e.Slug)
		if err != nil {
			s.serverError(w, err)
			return
		}
		tmpl := "task-row"
		if r.FormValue("from") == "card" {
			tmpl = "list-card"
		}
		s.pages.renderPartial(w, "list", tmpl, map[string]any{"E": fresh})
		return
	}

	http.Redirect(w, r, redirectBack(r, "/e/"+e.Slug), http.StatusSeeOther)
}

func (s *Server) handleEntryPin(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	if err := s.db.SetPinned(u.ID, e.ID, !e.Pinned); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/e/"+e.Slug), http.StatusSeeOther)
}

func (s *Server) handleEntryDelete(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	if err := s.db.DeleteEntry(u.ID, e.ID); err != nil {
		s.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/board")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/board", http.StatusSeeOther)
}

func redirectBack(r *http.Request, fallback string) string {
	if ref := r.Header.Get("Referer"); ref != "" {
		return ref
	}
	return fallback
}
