package web

import (
	"net/http"

	"taskmanager/internal/store"
)

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)

	var projectID int64
	projectSlug := r.URL.Query().Get("project")
	if projectSlug != "" {
		if p, err := s.db.ProjectBySlug(u.ID, projectSlug); err == nil {
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

	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("partial") == "board" {
		s.pages.renderPartial(w, "board", "board", data)
		return
	}
	s.pages.render(w, http.StatusOK, "board", data)
}

func (s *Server) handleEntryMove(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}

	status := r.FormValue("status")
	var beforeID int64
	if bs := r.FormValue("before"); bs != "" {
		if be, err := s.db.EntryBySlug(u.ID, bs); err == nil {
			beforeID = be.ID
		}
	}
	if err := s.db.MoveEntry(u.ID, e.ID, status, beforeID); err != nil {
		s.serverError(w, err)
		return
	}

	projectID := int64(0)
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
}
