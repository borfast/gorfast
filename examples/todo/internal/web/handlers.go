package web

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"uuid"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/examples/todo/internal/todo"
)

// The messages people see. None says more than the person needs.
const (
	msgWrongCredentials = "Wrong email or password."
	msgRateLimited      = "Too many attempts. Try again later."
	msgNotFound         = "That page does not exist."
	msgInternal         = "Something went wrong."
	msgTooLarge         = "The form is too large."
	msgBadForm          = "The form could not be read."
)

var (
	msgTextInvalid = fmt.Sprintf("A to-do must be 1 to %d characters.", todo.MaxTextLength)
	msgTooMany     = fmt.Sprintf("You have %d to-dos, which is the most allowed. Delete one to add another.", todo.MaxTodos)
)

// wrongCredentials are the sign-in refusals that all show msgWrongCredentials,
// so the page never says whether the email exists.
var wrongCredentials = []error{
	sulis.ErrInvalidCredentials,
	sulis.ErrInvalidEmail,
	sulis.ErrAccountDisabled,
	sulis.ErrAccountLocked,
	sulis.ErrEmailNotVerified,
}

// signUpMessages are the sign-up refusals and what each one shows.
var signUpMessages = []struct {
	err     error
	message string
}{
	{sulis.ErrUserAlreadyExists, "An account with this email address already exists."},
	{sulis.ErrInvalidEmail, "Enter a valid email address."},
	{sulis.ErrPasswordTooShort, "Your password is too short. Use at least 12 characters."},
	{sulis.ErrPasswordTooLong, "Your password is too long."},
	{sulis.ErrPasswordCompromised, "This password is on a list of common or leaked passwords. Choose another one."},
	{todo.ErrNameInvalid, fmt.Sprintf("Enter a name of 1 to %d characters.", todo.MaxNameLength)},
}

// The data each template renders.
type (
	accountPage struct{ CSRF, Message, Email, Name string }
	listPage    struct {
		CSRF, Message, Name, Query, Sort, Text string
		Todos                                  []todo.Todo
	}
	editPage struct {
		CSRF, Message, Text string
		ID                  uuid.UUID
	}
	errorPage struct{ Message string }
)

func (h *handler) signUpForm(w http.ResponseWriter, r *http.Request) {
	h.renderAccount(w, r, "signup", http.StatusOK, accountPage{})
}

func (h *handler) signUp(w http.ResponseWriter, r *http.Request) {
	email, name := r.PostFormValue("email"), r.PostFormValue("name")
	in, err := h.svc.SignUp(r.Context(), email, r.PostFormValue("password"), name, requestInfo(r))
	if err != nil {
		for _, m := range signUpMessages {
			if errors.Is(err, m.err) {
				h.renderAccount(w, r, "signup", http.StatusUnprocessableEntity, accountPage{Message: m.message, Email: email, Name: name})
				return
			}
		}
		h.fail(w, r, err)
		return
	}
	h.startSession(w, r, in)
}

func (h *handler) signInForm(w http.ResponseWriter, r *http.Request) {
	h.renderAccount(w, r, "signin", http.StatusOK, accountPage{})
}

func (h *handler) signIn(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	in, err := h.svc.SignIn(r.Context(), email, r.PostFormValue("password"), requestInfo(r))
	switch {
	case err == nil:
		h.startSession(w, r, in)
	case errors.Is(err, sulis.ErrRateLimited):
		h.renderAccount(w, r, "signin", http.StatusTooManyRequests, accountPage{Message: msgRateLimited, Email: email})
	case slices.ContainsFunc(wrongCredentials, func(e error) bool { return errors.Is(err, e) }):
		h.renderAccount(w, r, "signin", http.StatusUnprocessableEntity, accountPage{Message: msgWrongCredentials, Email: email})
	default:
		h.fail(w, r, err)
	}
}

// startSession sets the cookie for a session the service has committed.
func (h *handler) startSession(w http.ResponseWriter, r *http.Request, in todo.SignedIn) {
	http.SetCookie(w, h.auth.SessionCookie(in.Token, in.Expires))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) signOut(w http.ResponseWriter, r *http.Request) {
	s := current(r)
	// A session already gone, such as one revoked in another tab, is signed out.
	if err := h.svc.SignOut(r.Context(), s.userID, s.sessionID); err != nil && !errors.Is(err, sulis.ErrSessionNotFound) {
		h.fail(w, r, err)
		return
	}
	http.SetCookie(w, h.auth.ClearSessionCookie())
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	h.renderList(w, r, http.StatusOK, listPage{Query: q.Get("q"), Sort: q.Get("sort")})
}

func (h *handler) add(w http.ResponseWriter, r *http.Request) {
	text := r.PostFormValue("text")
	_, err := h.svc.Add(r.Context(), current(r).userID, text)
	switch {
	case err == nil:
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, todo.ErrTextInvalid):
		h.renderList(w, r, http.StatusUnprocessableEntity, listPage{Message: msgTextInvalid, Text: text})
	case errors.Is(err, todo.ErrTooMany):
		h.renderList(w, r, http.StatusUnprocessableEntity, listPage{Message: msgTooMany, Text: text})
	default:
		h.fail(w, r, err)
	}
}

func (h *handler) editForm(w http.ResponseWriter, r *http.Request) {
	id, ok := todoID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	// List returns only this user's to-dos, so another user's ID is not found.
	todos, err := h.svc.List(r.Context(), current(r).userID, "", todo.ByCreated)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	i := slices.IndexFunc(todos, func(t todo.Todo) bool { return t.ID == id })
	if i < 0 {
		h.notFound(w, r)
		return
	}
	h.renderEdit(w, r, http.StatusOK, editPage{ID: id, Text: todos[i].Text})
}

func (h *handler) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := todoID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	text := r.PostFormValue("text")
	err := h.svc.Edit(r.Context(), current(r).userID, id, text)
	if errors.Is(err, todo.ErrTextInvalid) {
		h.renderEdit(w, r, http.StatusUnprocessableEntity, editPage{ID: id, Text: text, Message: msgTextInvalid})
		return
	}
	h.done(w, r, err)
}

func (h *handler) setDone(w http.ResponseWriter, r *http.Request) {
	id, ok := todoID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var done bool
	switch r.PostFormValue("done") {
	case "true":
		done = true
	case "false":
	default:
		h.renderError(w, r, http.StatusBadRequest, msgBadForm)
		return
	}
	h.done(w, r, h.svc.SetDone(r.Context(), current(r).userID, id, done))
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := todoID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	h.done(w, r, h.svc.Delete(r.Context(), current(r).userID, id))
}

// done finishes a change to one to-do: back to the list, or the error page.
func (h *handler) done(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, todo.ErrNotFound):
		h.notFound(w, r)
	default:
		h.fail(w, r, err)
	}
}

func (h *handler) style(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(stylesheet)
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, r, http.StatusNotFound, msgNotFound)
}

// fail logs err with the method and path, never the query string, and shows
// a page that says nothing about it.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "web: request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	h.renderError(w, r, http.StatusInternalServerError, msgInternal)
}

// todoID parses the {id} in the path. A malformed ID is a page that does not exist.
func todoID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	return id, err == nil
}

// requestInfo takes the IP from the connection, never from a header the
// client chooses.
func requestInfo(r *http.Request) sulis.RequestInfo {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return sulis.RequestInfo{IP: ip, UserAgent: r.UserAgent()}
}

// csrfToken returns the token for a page's forms. It reuses the cookie's
// token, so a form in another tab keeps working, or sets a new one.
func (h *handler) csrfToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(h.csrfCookie); err == nil && c.Value != "" {
		return c.Value, nil
	}
	token, cookie, err := h.auth.IssueCSRFToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, cookie)
	return token, nil
}

func (h *handler) renderAccount(w http.ResponseWriter, r *http.Request, page string, status int, data accountPage) {
	token, err := h.csrfToken(w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data.CSRF = token
	h.render(w, r, page, status, data)
}

func (h *handler) renderList(w http.ResponseWriter, r *http.Request, status int, data listPage) {
	ctx, userID := r.Context(), current(r).userID
	if data.Sort != string(todo.ByText) {
		data.Sort = string(todo.ByCreated)
	}
	name, err := h.svc.Name(ctx, userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	todos, err := h.svc.List(ctx, userID, data.Query, todo.Order(data.Sort))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	token, err := h.csrfToken(w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data.CSRF, data.Name, data.Todos = token, name, todos
	h.render(w, r, "list", status, data)
}

func (h *handler) renderEdit(w http.ResponseWriter, r *http.Request, status int, data editPage) {
	token, err := h.csrfToken(w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data.CSRF = token
	h.render(w, r, "edit", status, data)
}

func (h *handler) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	h.render(w, r, "error", status, errorPage{Message: message})
}

// render writes the page only once the whole template has run, so a template
// error never sends half a page.
func (h *handler) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
	var buf bytes.Buffer
	if err := h.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		h.log.ErrorContext(r.Context(), "web: rendering a page", "page", page, "path", r.URL.Path, "error", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
