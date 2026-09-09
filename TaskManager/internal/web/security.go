package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

const csrfCookie = "cairn_csrf"
const csrfField = "_csrf"
const csrfHeader = "X-CSRF-Token"

const csrfKey ctxKey = 2

// isSecureRequest reports whether the connection reaching us is HTTPS, directly
// or via a terminating proxy.
func (s *Server) isSecureRequest(r *http.Request) bool {
	if s.forceSecureCookies {
		return true
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// guard is the outermost application middleware: it sets security headers,
// ensures a CSRF cookie exists (stashing its value in the request context for
// templates), and rejects state-changing requests whose token doesn't match.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")

		token := ""
		if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) >= 32 {
			token = c.Value
		} else {
			raw := make([]byte, 32)
			_, _ = rand.Read(raw)
			token = base64.RawURLEncoding.EncodeToString(raw)
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookie,
				Value:    token,
				Path:     "/",
				HttpOnly: false,
				Secure:   s.isSecureRequest(r),
				SameSite: http.SameSiteLaxMode,
				MaxAge:   30 * 24 * 3600,
			})
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfKey, token))

		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		sent := r.Header.Get(csrfHeader)
		if sent == "" {
			sent = r.FormValue(csrfField)
		}
		if sent == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
			http.Error(w, "bad or missing CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfFromRequest(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}
