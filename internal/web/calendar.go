package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"taskmanager/internal/parse"
	"taskmanager/internal/store"
)

func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)

	view := r.URL.Query().Get("view")
	switch view {
	case "day", "week", "month":
	default:
		view = "month"
	}
	anchor := parseAnchor(r.URL.Query().Get("date"))
	from, to := calRange(view, anchor)

	occ, err := s.db.OccurrencesInRange(u.ID, from, to, false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	cals, err := s.db.Calendars(u.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	data := s.base(r, "Calendar", "dense")
	data["Nav"] = buildCalNav(view, anchor)
	data["View"] = view
	data["Calendars"] = cals
	data["Hours"] = hoursOfDay()
	data["Colors"] = store.CalendarColors
	switch view {
	case "month":
		data["Weeks"] = buildMonth(anchor, occ)
	default:
		data["Grid"] = buildGrid(view, anchor, occ)
	}

	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("partial") == "1" {
		s.pages.renderPartial(w, "calendar", "calendar", data)
		return
	}
	s.pages.render(w, http.StatusOK, "calendar", data)
}

// renderCalendarFragment re-renders the #calendar region after a mutation.
func (s *Server) renderCalendarFragment(w http.ResponseWriter, r *http.Request, view string, anchor time.Time) {
	u := currentUser(r)
	from, to := calRange(view, anchor)
	occ, err := s.db.OccurrencesInRange(u.ID, from, to, false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	cals, _ := s.db.Calendars(u.ID)
	data := s.base(r, "Calendar", "dense")
	data["Nav"] = buildCalNav(view, anchor)
	data["View"] = view
	data["Calendars"] = cals
	data["Hours"] = hoursOfDay()
	data["Colors"] = store.CalendarColors
	if view == "month" {
		data["Weeks"] = buildMonth(anchor, occ)
	} else {
		data["Grid"] = buildGrid(view, anchor, occ)
	}
	s.pages.renderPartial(w, "calendar", "calendar", data)
}

func viewAndAnchor(r *http.Request) (string, time.Time) {
	view := r.FormValue("view")
	switch view {
	case "day", "week", "month":
	default:
		view = "month"
	}
	return view, parseAnchor(r.FormValue("date"))
}

// ---- calendars ---------------------------------------------------------

func (s *Server) handleCalendarCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	color := r.FormValue("color")
	if name == "" {
		http.Redirect(w, r, "/calendar", http.StatusSeeOther)
		return
	}
	if _, err := s.db.CreateCalendar(u.ID, name, color, "events", ""); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

func (s *Server) handleCalendarUpdate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.db.UpdateCalendar(u.ID, id, strings.TrimSpace(r.FormValue("name")), r.FormValue("color")); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

func (s *Server) handleCalendarVisible(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	visible := r.FormValue("visible") == "1" || r.FormValue("visible") == "on"
	if err := s.db.SetCalendarVisible(u.ID, id, visible); err != nil {
		s.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		view, anchor := viewAndAnchor(r)
		s.renderCalendarFragment(w, r, view, anchor)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

func (s *Server) handleCalendarDelete(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.db.DeleteCalendar(u.ID, id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

// handleCalendarImport accepts an uploaded .ics file.
func (s *Server) handleCalendarImport(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "upload too large or malformed", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "choose an .ics file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	cal, err := parse.ParseICS(file)
	if err != nil {
		http.Error(w, "could not read that .ics file", http.StatusBadRequest)
		return
	}

	target := r.FormValue("calendar_id") // "" or "new"
	var calID int64
	if id, perr := strconv.ParseInt(target, 10, 64); perr == nil && id > 0 {
		existing, gerr := s.db.CalendarByID(u.ID, id)
		if gerr != nil {
			s.notFound(w, r)
			return
		}
		calID = existing.ID
	} else {
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = cal.Name
		}
		if name == "" {
			name = strings.TrimSuffix(header.Filename, ".ics")
		}
		created, cerr := s.db.CreateCalendar(u.ID, name, pickColor(u.ID, s), "imported", cal.Name)
		if cerr != nil {
			s.serverError(w, cerr)
			return
		}
		calID = created.ID
	}

	n, err := s.db.ImportICS(u.ID, calID, cal)
	if err != nil {
		s.serverError(w, err)
		return
	}
	_ = n
	http.Redirect(w, r, "/calendar", http.StatusSeeOther)
}

func pickColor(userID int64, s *Server) string {
	cals, _ := s.db.Calendars(userID)
	used := map[string]bool{}
	for _, c := range cals {
		used[c.Color] = true
	}
	for _, c := range store.CalendarColors {
		if !used[c] {
			return c
		}
	}
	return store.CalendarColors[len(cals)%len(store.CalendarColors)]
}

// ---- events ----------------------------------------------------------

func eventInputFromForm(r *http.Request) (store.EventInput, error) {
	calID, _ := strconv.ParseInt(r.FormValue("calendar_id"), 10, 64)
	allDay := r.FormValue("all_day") == "on" || r.FormValue("all_day") == "1"

	in := store.EventInput{
		CalendarID:  calID,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: r.FormValue("description"),
		Location:    strings.TrimSpace(r.FormValue("location")),
		AllDay:      allDay,
		RRule:       strings.TrimSpace(r.FormValue("rrule")),
	}

	date := r.FormValue("date")
	if date == "" {
		return in, errors.New("date required")
	}
	if allDay {
		start, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			return in, err
		}
		in.Start = start
		end := start.AddDate(0, 0, 1)
		if ed := r.FormValue("end_date"); ed != "" {
			if e, perr := time.ParseInLocation("2006-01-02", ed, time.Local); perr == nil && !e.Before(start) {
				end = e.AddDate(0, 0, 1)
			}
		}
		in.End = end
		return in, nil
	}

	st := r.FormValue("start_time")
	if st == "" {
		st = "09:00"
	}
	et := r.FormValue("end_time")
	if et == "" {
		et = "10:00"
	}
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+st, time.Local)
	if err != nil {
		return in, err
	}
	end, err := time.ParseInLocation("2006-01-02 15:04", date+" "+et, time.Local)
	if err != nil || !end.After(start) {
		end = start.Add(time.Hour)
	}
	in.Start, in.End = start, end
	return in, nil
}

func (s *Server) handleEventCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	in, err := eventInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.CalendarID == 0 {
		if cals, _ := s.db.Calendars(u.ID); len(cals) > 0 {
			for _, c := range cals {
				if c.Kind != "tasks" {
					in.CalendarID = c.ID
					break
				}
			}
		}
	}
	if in.Title == "" {
		in.Title = "(untitled)"
	}
	if _, err := s.db.CreateEvent(u.ID, in); err != nil {
		s.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		view, anchor := viewAndAnchor(r)
		s.renderCalendarFragment(w, r, view, anchor)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

func (s *Server) handleEventEdit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	e, err := s.db.EventByID(u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	cals, _ := s.db.Calendars(u.ID)
	data := s.base(r, e.Title, "minimal")
	data["E"] = e
	data["Calendars"] = cals
	data["Back"] = r.URL.Query().Get("back")
	s.pages.render(w, http.StatusOK, "event", data)
}

func (s *Server) handleEventSave(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	in, err := eventInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.db.UpdateEvent(u.ID, id, in); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/calendar", http.StatusSeeOther)
}

func (s *Server) handleEventMove(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	date := r.FormValue("date")
	if err := s.db.MoveEvent(u.ID, id, date); err != nil {
		s.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		view, anchor := viewAndAnchor(r)
		s.renderCalendarFragment(w, r, view, anchor)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}

func (s *Server) handleEventDelete(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.db.DeleteEvent(u.ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/calendar", http.StatusSeeOther)
}

// handleEntryDue sets a task's due date (calendar drag / day planning).
func (s *Server) handleEntryDue(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	e := s.lookupEntry(w, r)
	if e == nil {
		return
	}
	due := strings.TrimSpace(r.FormValue("due"))
	in := store.EntryInput{
		Title: e.Title, Body: e.Body, Status: e.Status, Priority: e.Priority,
		SetDue: true, Due: due,
	}
	if e.Status == "" && due != "" {
		in.Status = "next" // giving a bare note a date makes it a task to do
	}
	if _, err := s.db.UpdateEntry(u.ID, e.ID, in); err != nil {
		s.serverError(w, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		view, anchor := viewAndAnchor(r)
		s.renderCalendarFragment(w, r, view, anchor)
		return
	}
	http.Redirect(w, r, redirectBack(r, "/calendar"), http.StatusSeeOther)
}
