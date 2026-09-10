package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"taskmanager/internal/parse"
	"taskmanager/internal/store"
)

type pages struct {
	set   map[string]*template.Template
	funcs template.FuncMap
}

func parsePages(fsys fs.FS) (*pages, error) {
	funcs := template.FuncMap{
		"dateLabel":     dateLabel,
		"dueClass":      dueClass,
		"relTime":       relTime,
		"priorityLabel": priorityLabel,
		"priorityClass": priorityClass,
		"statusLabel":   func(s string) string { return store.StatusLabels[s] },
		"doneCount":     doneCount,
		"excerpt":       excerpt,
		"initial":       initial,
		"add":           func(a, b int) int { return a + b },
		"sub":           func(a, b int) int { return a - b },
		"lanePct":       func(lane, lanes int) float64 { return float64(lane) / float64(max1(lanes)) * 100 },
		"widthPct":      func(lanes int) float64 { return 100 / float64(max1(lanes)) },
		"dict":          dict,
		"hasPrefix":     strings.HasPrefix,
		"join":          strings.Join,
	}

	base, err := template.New("base").Funcs(funcs).ParseFS(fsys,
		"templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}

	pageFiles, err := fs.Glob(fsys, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	p := &pages{set: make(map[string]*template.Template, len(pageFiles)), funcs: funcs}
	for _, f := range pageFiles {
		name := strings.TrimSuffix(pathBase(f), ".html")
		clone, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := clone.ParseFS(fsys, f); err != nil {
			return nil, err
		}
		p.set[name] = clone
	}
	return p, nil
}

func (p *pages) render(w http.ResponseWriter, status int, name string, data any) {
	t, ok := p.set[name]
	if !ok {
		http.Error(w, "unknown page: "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (p *pages) renderPartial(w http.ResponseWriter, page, tmpl string, data any) {
	t, ok := p.set[page]
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, tmpl, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// ---- per-request base data ------------------------------------------------

func (s *Server) base(r *http.Request, title, defaultMode string) map[string]any {
	u := currentUser(r)
	data := map[string]any{
		"Title":    title,
		"SiteName": s.SiteName,
		"Path":     r.URL.Path,
		"User":     u,
		"Mode":     s.resolveMode(r, defaultMode),
		"CSRF":     csrfFromRequest(r),
	}
	if u != nil {
		projects, _ := s.db.Projects(u.ID)
		tags, _ := s.db.Tags(u.ID)
		data["Projects"] = projects
		data["Tags"] = tags
	}
	return data
}

func (s *Server) resolveMode(r *http.Request, def string) string {
	if c, err := r.Cookie(modeCookie); err == nil {
		switch c.Value {
		case "minimal", "dense":
			return c.Value
		}
	}
	if def == "" {
		return "dense"
	}
	return def
}

// bodyHTML renders an entry body to safe HTML, resolving [[wiki-links]] against
// the user's entries.
func (s *Server) bodyHTML(userID int64, body string) template.HTML {
	resolver := func(target string) (string, bool) {
		if slug, ok := s.db.TitleSlug(userID, target); ok {
			return "/e/" + slug, true
		}
		return "/e/" + store.Slugify(target), false
	}
	return template.HTML(parse.RenderHTML(body, resolver))
}

// ---- template funcs -----------------------------------------------------

func dateLabel(iso string) string {
	if iso == "" {
		return ""
	}
	d, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(d.Sub(today).Hours() / 24)
	switch {
	case days == 0:
		return "Today"
	case days == 1:
		return "Tomorrow"
	case days == -1:
		return "Yesterday"
	case days < -1:
		return fmt.Sprintf("%dd overdue", -days)
	case days < 7:
		return d.Format("Mon")
	case d.Year() == now.Year():
		return d.Format("Jan 2")
	default:
		return d.Format("Jan 2 2006")
	}
}

func dueClass(iso string) string {
	if iso == "" {
		return ""
	}
	d, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(d.Sub(today).Hours() / 24)
	switch {
	case days < 0:
		return "due-overdue"
	case days == 0:
		return "due-today"
	case days <= 3:
		return "due-soon"
	default:
		return "due-later"
	}
}

func relTime(ts string) string {
	t, err := time.Parse("2006-01-02 15:04:05", ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

func priorityLabel(p int) string {
	switch p {
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	}
	return ""
}

func priorityClass(p int) string {
	switch p {
	case 1:
		return "pri-low"
	case 2:
		return "pri-med"
	case 3:
		return "pri-high"
	}
	return ""
}

var excerptStripper = strings.NewReplacer(
	"[[", "", "]]", "", "**", "", "`", "", "> ", "", "# ", "", "## ", "", "### ", "",
)

// excerpt collapses an entry body to a short, mostly-plain preview line.
func excerpt(s string, n int) string {
	s = excerptStripper.Replace(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := n
	for cut > 0 && r[cut] != ' ' {
		cut--
	}
	if cut == 0 {
		cut = n
	}
	return strings.TrimSpace(string(r[:cut])) + "…"
}

func doneCount(es []*store.Entry) int {
	n := 0
	for _, e := range es {
		if e.Status == "done" {
			n++
		}
	}
	return n
}

func initial(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "?"
	}
	return strings.ToUpper(email[:1])
}

func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is not a string", i)
		}
		m[k] = pairs[i+1]
	}
	return m, nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func pathBase(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}
