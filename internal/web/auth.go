package web

import (
	"errors"
	"net/http"
	"strings"

	"taskmanager/internal/store"
)

func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/board"
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) != nil {
		http.Redirect(w, r, "/board", http.StatusSeeOther)
		return
	}
	s.pages.render(w, http.StatusOK, "login", map[string]any{
		"Title": "Sign in", "SiteName": s.SiteName, "Mode": "minimal", "CSRF": csrfFromRequest(r),
		"Next": r.URL.Query().Get("next"),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	u, err := s.db.Authenticate(email, password)
	if err != nil {
		s.pages.render(w, http.StatusUnauthorized, "login", map[string]any{
			"Title": "Sign in", "SiteName": s.SiteName, "Mode": "minimal", "CSRF": csrfFromRequest(r),
			"Error": "Email or password is incorrect.", "Email": email, "Next": next,
		})
		return
	}
	token, err := s.db.CreateSession(u.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleSignupForm(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) != nil {
		http.Redirect(w, r, "/board", http.StatusSeeOther)
		return
	}
	s.pages.render(w, http.StatusOK, "signup", map[string]any{
		"Title": "Create account", "SiteName": s.SiteName, "Mode": "minimal", "CSRF": csrfFromRequest(r),
	})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	name := r.FormValue("name")
	password := r.FormValue("password")

	fail := func(msg string) {
		s.pages.render(w, http.StatusBadRequest, "signup", map[string]any{
			"Title": "Create account", "SiteName": s.SiteName, "Mode": "minimal", "CSRF": csrfFromRequest(r),
			"Error": msg, "Email": email, "Name": name,
		})
	}

	if len(password) < 8 {
		fail("Password must be at least 8 characters.")
		return
	}
	u, err := s.db.CreateUser(email, name, password)
	if errors.Is(err, store.ErrEmailTaken) {
		fail("That email is already registered.")
		return
	}
	if err != nil {
		fail(err.Error())
		return
	}
	token, err := s.db.CreateSession(u.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	http.Redirect(w, r, "/board", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.db.DeleteSession(c.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	mode := r.FormValue("mode")
	if mode != "minimal" && mode != "dense" {
		mode = "dense"
	}
	http.SetCookie(w, &http.Cookie{
		Name: modeCookie, Value: mode, Path: "/", SameSite: http.SameSiteLaxMode,
		Secure: s.isSecureRequest(r), MaxAge: 365 * 24 * 3600,
	})
	back := r.Header.Get("Referer")
	if back == "" {
		back = "/board"
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
