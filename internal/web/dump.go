package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"taskmanager/internal/parse"
	"taskmanager/internal/store"
)

func (s *Server) handleDump(w http.ResponseWriter, r *http.Request) {
	data := s.base(r, "Idea dump", "minimal")
	s.pages.render(w, http.StatusOK, "dump", data)
}

// handleDumpPreview parses the pasted text and returns an editable review form.
func (s *Server) handleDumpPreview(w http.ResponseWriter, r *http.Request) {
	text := r.FormValue("text")
	items := parse.Dump(text, time.Now())

	rows := make([]map[string]any, 0, len(items))
	total := 0
	row := func(it parse.Item, id, parent string, depth int) {
		status := ""
		if it.IsTask {
			status = "inbox"
			if it.Done {
				status = "done"
			}
		}
		rows = append(rows, map[string]any{
			"I":        id,
			"Parent":   parent,
			"Depth":    depth,
			"Title":    it.Title,
			"Body":     it.Body,
			"Status":   status,
			"IsTask":   it.IsTask,
			"Priority": it.Priority,
			"Due":      it.Due,
			"Project":  it.Project,
			"Tags":     it.Tags,
			"Links":    it.Links,
		})
		total++
	}
	for i, it := range items {
		pid := strconv.Itoa(i)
		row(it, pid, "", 0)
		for j, sub := range it.Sub {
			row(sub, pid+"-"+strconv.Itoa(j), pid, 1)
		}
	}
	data := s.base(r, "Idea dump", "minimal")
	data["Rows"] = rows
	data["Count"] = total
	data["Raw"] = text
	data["Statuses"] = store.Statuses
	data["Empty"] = len(rows) == 0
	s.pages.renderPartial(w, "dump", "preview", data)
}

// handleDumpCommit creates entries from the reviewed rows.
func (s *Server) handleDumpCommit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := r.ParseForm(); err != nil {
		s.serverError(w, err)
		return
	}

	count := 0
	created := map[string]int64{} // row id -> new entry id, so subtasks can link
	for _, i := range r.Form["row"] {
		if r.FormValue("keep_"+i) == "" {
			continue
		}
		title := strings.TrimSpace(r.FormValue("title_" + i))
		body := r.FormValue("body_" + i)
		if title == "" && strings.TrimSpace(body) == "" {
			continue
		}
		priority, _ := strconv.Atoi(r.FormValue("priority_" + i))
		in := store.EntryInput{
			Title:      title,
			Body:       body,
			Status:     r.FormValue("status_" + i),
			Priority:   priority,
			Due:        strings.TrimSpace(r.FormValue("due_" + i)),
			Project:    strings.TrimSpace(r.FormValue("project_" + i)),
			SetDue:     true,
			SetProject: true,
		}
		if parent := r.FormValue("parent_" + i); parent != "" {
			if pid, ok := created[parent]; ok {
				in.ParentID = pid
			}
		}
		e, err := s.db.CreateEntry(u.ID, in)
		if err != nil {
			s.serverError(w, err)
			return
		}
		created[i] = e.ID
		count++
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/board")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/board", http.StatusSeeOther)
}
