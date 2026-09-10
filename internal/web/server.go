// Package web wires HTTP routing, templates and static assets together.
package web

import (
	"embed"
	"io/fs"
	"net/http"

	"taskmanager/internal/store"
)

//go:embed templates static
var assetFS embed.FS

// Server holds the HTTP dependencies.
type Server struct {
	db                 *store.DB
	pages              *pages
	mux                *http.ServeMux
	handler            http.Handler
	secret             []byte
	forceSecureCookies bool
	SiteName           string
}

// Config carries the knobs New needs.
type Config struct {
	SiteName      string
	Secret        []byte
	SecureCookies bool
}

// New builds a Server backed by db.
func New(db *store.DB, cfg Config) (*Server, error) {
	p, err := parsePages(assetFS)
	if err != nil {
		return nil, err
	}
	s := &Server{
		db:                 db,
		pages:              p,
		mux:                http.NewServeMux(),
		secret:             cfg.Secret,
		forceSecureCookies: cfg.SecureCookies,
		SiteName:           cfg.SiteName,
	}
	s.routes()
	s.handler = s.guard(s.mux)
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() {
	staticFS, _ := fs.Sub(assetFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", cacheControl(http.FileServer(http.FS(staticFS)))))

	// Public.
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("GET /signup", s.handleSignupForm)
	s.mux.HandleFunc("POST /signup", s.handleSignup)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Authenticated.
	s.mux.Handle("GET /{$}", s.requireUser(s.handleHome))
	s.mux.Handle("GET /board", s.requireUser(s.handleBoard))
	s.mux.Handle("GET /list", s.requireUser(s.handleList))
	s.mux.Handle("GET /dump", s.requireUser(s.handleDump))
	s.mux.Handle("POST /dump/preview", s.requireUser(s.handleDumpPreview))
	s.mux.Handle("POST /dump/commit", s.requireUser(s.handleDumpCommit))
	s.mux.Handle("GET /search", s.requireUser(s.handleSearch))

	s.mux.Handle("POST /entries", s.requireUser(s.handleEntryCreate))
	s.mux.Handle("GET /e/{slug}", s.requireUser(s.handleEntry))
	s.mux.Handle("GET /e/{slug}/edit", s.requireUser(s.handleEntryEdit))
	s.mux.Handle("POST /e/{slug}", s.requireUser(s.handleEntrySave))
	s.mux.Handle("POST /e/{slug}/move", s.requireUser(s.handleEntryMove))
	s.mux.Handle("POST /e/{slug}/due", s.requireUser(s.handleEntryDue))
	s.mux.Handle("POST /e/{slug}/toggle", s.requireUser(s.handleEntryToggle))
	s.mux.Handle("POST /e/{slug}/pin", s.requireUser(s.handleEntryPin))
	s.mux.Handle("POST /e/{slug}/delete", s.requireUser(s.handleEntryDelete))

	s.mux.Handle("GET /projects/{slug}", s.requireUser(s.handleProject))
	s.mux.Handle("POST /projects", s.requireUser(s.handleProjectCreate))
	s.mux.Handle("GET /tags/{name}", s.requireUser(s.handleTag))

	s.mux.Handle("POST /mode", s.requireUser(s.handleMode))

	// Calendar.
	s.mux.Handle("GET /calendar", s.requireUser(s.handleCalendar))
	s.mux.Handle("POST /calendars", s.requireUser(s.handleCalendarCreate))
	s.mux.Handle("POST /calendars/import", s.requireUser(s.handleCalendarImport))
	s.mux.Handle("POST /calendars/{id}", s.requireUser(s.handleCalendarUpdate))
	s.mux.Handle("POST /calendars/{id}/visible", s.requireUser(s.handleCalendarVisible))
	s.mux.Handle("POST /calendars/{id}/delete", s.requireUser(s.handleCalendarDelete))

	s.mux.Handle("POST /events", s.requireUser(s.handleEventCreate))
	s.mux.Handle("GET /events/{id}", s.requireUser(s.handleEventEdit))
	s.mux.Handle("POST /events/{id}", s.requireUser(s.handleEventSave))
	s.mux.Handle("POST /events/{id}/move", s.requireUser(s.handleEventMove))
	s.mux.Handle("POST /events/{id}/delete", s.requireUser(s.handleEventDelete))
}

func cacheControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}
