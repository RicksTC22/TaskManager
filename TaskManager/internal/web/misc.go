package web

import (
	"errors"
	"log"
	"net/http"

	"taskmanager/internal/store"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/board", http.StatusSeeOther)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	q := r.URL.Query().Get("q")
	var results []*store.Entry
	if q != "" {
		var err error
		results, err = s.db.ListEntries(u.ID, store.EntryFilter{Query: q, Sort: "updated", Limit: 100})
		if err != nil {
			s.serverError(w, err)
			return
		}
	}
	data := s.base(r, "Search", "dense")
	data["Query"] = q
	data["Results"] = results
	if htmxPartial(r) {
		s.pages.renderPartial(w, "search", "results", data)
		return
	}
	s.pages.render(w, http.StatusOK, "search", data)
}

func (s *Server) handleTag(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := r.PathValue("name")
	entries, err := s.db.ListEntries(u.ID, store.EntryFilter{Tag: name, Sort: "updated"})
	if err != nil {
		s.serverError(w, err)
		return
	}
	data := s.base(r, "#"+name, "dense")
	data["Heading"] = "#" + name
	data["Entries"] = entries
	s.pages.render(w, http.StatusOK, "collection", data)
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p, err := s.db.ProjectBySlug(u.ID, r.PathValue("slug"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	entries, err := s.db.ListEntries(u.ID, store.EntryFilter{ProjectID: p.ID, Sort: "due"})
	if err != nil {
		s.serverError(w, err)
		return
	}
	data := s.base(r, p.Name, "dense")
	data["Heading"] = p.Name
	data["Project"] = p
	data["Entries"] = entries
	s.pages.render(w, http.StatusOK, "collection", data)
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/list", http.StatusSeeOther)
		return
	}
	p, err := s.db.CreateProject(u.ID, name)
	if err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+p.Slug, http.StatusSeeOther)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.base(r, "Not found", "dense")
	s.pages.render(w, http.StatusNotFound, "notfound", data)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
