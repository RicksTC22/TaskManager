package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"taskmanager/internal/store"
)

const sessionCookie = "cairn_session"
const modeCookie = "cairn_mode"

type ctxKey int

const userKey ctxKey = 1

// RequestLog logs one line per request.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// requireUser wraps a handler, redirecting to /login when there is no session.
func (s *Server) requireUser(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.userFromRequest(r)
		if u == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		h(w, r.WithContext(ctx))
	})
}

func (s *Server) userFromRequest(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	u, err := s.db.UserBySession(c.Value)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		log.Printf("session lookup: %v", err)
		return nil
	}
	return u
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// htmxPartial reports whether this is a real in-page htmx request (a fragment
// swap) rather than an hx-boost full-page navigation, which also carries the
// HX-Request header but expects a complete page.
func htmxPartial(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true"
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
}
