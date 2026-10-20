package web

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/borfast/sulis"
)

const contentSecurityPolicy = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// securityHeaders sets the headers every response carries. Nothing here is
// worth caching, so no response may be stored.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// readForm parses a POST body before the CSRF check reads its token, so a body
// over the limit is refused here. Parse errors are not logged: they hold form text.
func (h *handler) readForm(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					h.renderError(w, r, http.StatusRequestEntityTooLarge, msgTooLarge)
					return
				}
				h.renderError(w, r, http.StatusBadRequest, msgBadForm)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// signedIn is who made a request, as requireSession found it.
type signedIn struct {
	userID, sessionID uuid.UUID
}

type signedInKey struct{}

// signedOutErrors are the ValidateSession errors that mean the person must
// sign in again. Any other error is the server's fault.
var signedOutErrors = []error{
	sulis.ErrSessionNotFound,
	sulis.ErrSessionExpired,
	sulis.ErrNotAuthenticated,
	sulis.ErrAccountDisabled,
	sulis.ErrUserNotFound,
}

// requireSession sends a request without a valid session to the sign-in page,
// and gives next the signed-in user and session in the request context.
func (h *handler) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(h.sessionCookie)
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		session, user, err := h.auth.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			for _, signedOut := range signedOutErrors {
				if errors.Is(err, signedOut) {
					http.SetCookie(w, h.auth.ClearSessionCookie())
					http.Redirect(w, r, "/signin", http.StatusSeeOther)
					return
				}
			}
			h.fail(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), signedInKey{}, signedIn{userID: user.ID, sessionID: session.ID})
		next(w, r.WithContext(ctx))
	})
}

// current returns who requireSession found. A handler that calls it must be
// behind requireSession.
func current(r *http.Request) signedIn {
	s, ok := r.Context().Value(signedInKey{}).(signedIn)
	if !ok {
		panic("web: a signed-in handler is not behind requireSession")
	}
	return s
}
