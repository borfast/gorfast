// Package web serves the example's pages: plain HTML forms rendered with
// html/template, and no JavaScript.
package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/examples/todo/internal/todo"
)

// SessionCookieName is the session cookie's name. Pass it to sulis.WithCookieName.
const SessionCookieName = "__Host-session"

// maxBodyBytes limits every request body.
const maxBodyBytes = 64 << 10

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/style.css
var stylesheet []byte

// pageNames are the templates under templates/, each rendered inside layout.html.
var pageNames = []string{"signup", "signin", "list", "edit", "error"}

type handler struct {
	svc   *todo.Service
	auth  *sulis.Sulis
	log   *slog.Logger
	pages map[string]*template.Template

	sessionCookie, csrfCookie string
}

// New returns the application's HTTP handler. auth signs people in and checks
// every POST; log receives errors that people see only as a generic page.
func New(svc *todo.Service, auth *sulis.Sulis, log *slog.Logger) (http.Handler, error) {
	if svc == nil || auth == nil || log == nil {
		return nil, errors.New("web: New needs a service, a Sulis and a logger")
	}
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	// Sulis does not expose its cookie names, so read them from cookies it makes.
	_, csrf, err := auth.IssueCSRFToken()
	if err != nil {
		return nil, fmt.Errorf("web: issuing a CSRF token: %w", err)
	}
	h := &handler{
		svc:           svc,
		auth:          auth,
		log:           log,
		pages:         pages,
		sessionCookie: auth.SessionCookie("", time.Time{}).Name,
		csrfCookie:    csrf.Name,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /signup", h.signUpForm)
	mux.HandleFunc("POST /signup", h.signUp)
	mux.HandleFunc("GET /signin", h.signInForm)
	mux.HandleFunc("POST /signin", h.signIn)
	mux.HandleFunc("GET /static/style.css", h.style)
	mux.Handle("GET /{$}", h.requireSession(h.list))
	mux.Handle("POST /signout", h.requireSession(h.signOut))
	mux.Handle("POST /todos", h.requireSession(h.add))
	mux.Handle("GET /todos/{id}/edit", h.requireSession(h.editForm))
	mux.Handle("POST /todos/{id}", h.requireSession(h.edit))
	mux.Handle("POST /todos/{id}/done", h.requireSession(h.setDone))
	mux.Handle("POST /todos/{id}/delete", h.requireSession(h.delete))
	mux.HandleFunc("/", h.notFound)

	// These wrap the whole mux, so no POST can reach a route without the
	// origin and CSRF checks. Both let GET through.
	var root http.Handler = auth.RequireCSRFToken(mux)
	root = h.readForm(root)
	root = auth.RequireSameOrigin(nil)(root)
	root = securityHeaders(root)
	return http.MaxBytesHandler(root, maxBodyBytes), nil
}

func parsePages() (map[string]*template.Template, error) {
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("web: parsing the %s page: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}
