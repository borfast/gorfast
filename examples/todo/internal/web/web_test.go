package web_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/examples/todo/internal/storage"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
	"github.com/borfast/gorfast/examples/todo/internal/web"
	"github.com/borfast/gorfast/internal/testdb"
)

const password = "correct-battery-staple"

const csp = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// logBuffer collects log lines, which the server writes from its own goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// fixture is one database behind a running server, wired as main wires it.
type fixture struct {
	t    *testing.T
	db   *bun.DB
	spec string
	base *sulis.Sulis
	svc  *todo.Service
	srv  *httptest.Server
	logs *logBuffer
}

// setup applies the migrations and starts the server, adding opts to the
// Sulis options every test uses.
func setup(t *testing.T, db *bun.DB, opts ...sulis.Option) *fixture {
	t.Helper()
	ctx := t.Context()
	if err := storage.Apply(ctx, db); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	key, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	spec := key.Spec()
	keys, err := crypt.Load(ctx, crypt.StaticKeys(spec))
	if err != nil {
		t.Fatalf("crypt.Load: %v", err)
	}

	logs := new(logBuffer)
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts = append([]sulis.Option{
		sulis.WithCookieName(web.SessionCookieName),
		sulis.WithEventSink(sulis.NewSlogSink(logger)),
		sulis.WithRequireVerifiedEmail(false),
		sulis.WithoutRateLimiting(),
		sulis.WithArgon2Params(sulis.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}),
	}, opts...)
	base, err := sulis.New(bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db), sulis.NoSecondFactors{}, opts...)
	if err != nil {
		t.Fatalf("sulis.New: %v", err)
	}
	svc := todo.NewService(data.NewBunRunner(db, storage.Binder(base, keys)))

	handler, err := web.New(svc, base, logger)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return &fixture{t: t, db: db, spec: spec, base: base, svc: svc, srv: srv, logs: logs}
}

// signUp registers email through the service, not the browser, and returns
// the new user's ID.
func (f *fixture) signUp(email, name string) uuid.UUID {
	f.t.Helper()
	in, err := f.svc.SignUp(f.t.Context(), email, password, name, sulis.RequestInfo{})
	if err != nil {
		f.t.Fatalf("SignUp %s: %v", email, err)
	}
	_, user, err := f.base.ValidateSession(f.t.Context(), in.Token)
	if err != nil {
		f.t.Fatalf("ValidateSession: %v", err)
	}
	return user.ID
}

func (f *fixture) add(owner uuid.UUID, text string) todo.Todo {
	f.t.Helper()
	item, err := f.svc.Add(f.t.Context(), owner, text)
	if err != nil {
		f.t.Fatalf("Add %q: %v", text, err)
	}
	return item
}

func (f *fixture) todos(owner uuid.UUID) []todo.Todo {
	f.t.Helper()
	todos, err := f.svc.List(f.t.Context(), owner, "", todo.ByCreated)
	if err != nil {
		f.t.Fatalf("List: %v", err)
	}
	return todos
}

type response struct {
	status int
	header http.Header
	body   string
}

func (r response) location() string { return r.header.Get("Location") }

// browser is a client with its own cookies. It does not follow redirects, so
// tests can check them.
type browser struct {
	f      *fixture
	client *http.Client
}

func (f *fixture) browser() *browser {
	f.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatalf("cookiejar.New: %v", err)
	}
	client := *f.srv.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{f: f, client: &client}
}

func (b *browser) do(method, path string, body io.Reader, header http.Header) response {
	t := b.f.t
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, b.f.srv.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	maps.Copy(req.Header, header)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatalf("%s %.100s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the response to %s %.100s: %v", method, path, err)
	}
	return response{status: res.StatusCode, header: res.Header, body: string(data)}
}

func (b *browser) get(path string) response {
	b.f.t.Helper()
	return b.do(http.MethodGet, path, nil, nil)
}

// postFrom posts form as a page served from origin would. site is the
// Sec-Fetch-Site value the browser sends, and an empty site sends none.
func (b *browser) postFrom(origin, site, path string, form url.Values) response {
	b.f.t.Helper()
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Origin": {origin}}
	if site != "" {
		header.Set("Sec-Fetch-Site", site)
	}
	return b.do(http.MethodPost, path, strings.NewReader(form.Encode()), header)
}

// post posts form as a page from the server itself would.
func (b *browser) post(path string, form url.Values) response {
	b.f.t.Helper()
	return b.postFrom(b.f.srv.URL, "same-origin", path, form)
}

var tokenField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// token GETs a form page and returns the CSRF token in its form.
func (b *browser) token(page string) string {
	t := b.f.t
	t.Helper()
	res := b.get(page)
	if res.status != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", page, res.status)
	}
	m := tokenField.FindStringSubmatch(res.body)
	if m == nil {
		t.Fatalf("GET %s: no csrf_token field in the page", page)
	}
	return m[1]
}

// submit fills in the form on page and posts it to action, as a person would.
func (b *browser) submit(page, action string, form url.Values) response {
	b.f.t.Helper()
	return b.post(action, withToken(form, b.token(page)))
}

func withToken(form url.Values, token string) url.Values {
	out := url.Values{"csrf_token": {token}}
	maps.Copy(out, form)
	return out
}

func (b *browser) cookie(name string) string {
	u, err := url.Parse(b.f.srv.URL)
	if err != nil {
		b.f.t.Fatalf("parsing the server URL: %v", err)
	}
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (b *browser) signUp(email, name string) {
	b.f.t.Helper()
	res := b.submit("/signup", "/signup", url.Values{"email": {email}, "password": {password}, "name": {name}})
	requireRedirect(b.f.t, "signing up", res, "/")
}

func (b *browser) signIn(email string) {
	b.f.t.Helper()
	res := b.submit("/signin", "/signin", url.Values{"email": {email}, "password": {password}})
	requireRedirect(b.f.t, "signing in", res, "/")
}

// session returns the browser's session and user, as the server sees them.
func (b *browser) session() (*sulis.Session, *sulis.User) {
	t := b.f.t
	t.Helper()
	token := b.cookie(web.SessionCookieName)
	if token == "" {
		t.Fatal("the browser has no session cookie")
	}
	session, user, err := b.f.base.ValidateSession(t.Context(), token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	return session, user
}

func requireStatus(t *testing.T, what string, res response, want int) {
	t.Helper()
	if res.status != want {
		t.Fatalf("%s = %d, want %d; body:\n%.2000s", what, res.status, want, res.body)
	}
}

func requireRedirect(t *testing.T, what string, res response, to string) {
	t.Helper()
	if res.status != http.StatusSeeOther || res.location() != to {
		t.Fatalf("%s = %d to %q, want 303 to %q; body:\n%.2000s", what, res.status, res.location(), to, res.body)
	}
}

func requireContains(t *testing.T, what, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("%s does not contain %q; body:\n%.2000s", what, want, body)
	}
}

var listedText = regexp.MustCompile(`<span class="text">([^<]*)</span>`)

// listed returns the to-do texts on a list page, in page order.
func listed(body string) []string {
	var texts []string
	for _, m := range listedText.FindAllStringSubmatch(body, -1) {
		texts = append(texts, m[1])
	}
	return texts
}

func TestSignUpListAndSignOut(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")

		page := b.get("/")
		requireStatus(t, "GET /", page, http.StatusOK)
		requireContains(t, "the list", page.body, "Alice")
		_, user := b.session()

		requireRedirect(t, "adding", b.submit("/", "/todos", url.Values{"text": {"Buy milk"}}), "/")
		todos := f.todos(user.ID)
		if len(todos) != 1 || todos[0].Text != "Buy milk" {
			t.Fatalf("to-dos after adding = %+v, want one, Buy milk", todos)
		}
		requireContains(t, "the list", b.get("/").body, "Buy milk")
		id := todos[0].ID.String()

		edit := "/todos/" + id + "/edit"
		page = b.get(edit)
		requireStatus(t, "GET "+edit, page, http.StatusOK)
		requireContains(t, "the edit page", page.body, `value="Buy milk"`)
		requireRedirect(t, "editing", b.submit(edit, "/todos/"+id, url.Values{"text": {"Buy oat milk"}}), "/")
		requireRedirect(t, "completing", b.submit("/", "/todos/"+id+"/done", url.Values{"done": {"true"}}), "/")
		if got := f.todos(user.ID); len(got) != 1 || got[0].Text != "Buy oat milk" || !got[0].Done {
			t.Fatalf("to-dos after editing and completing = %+v, want one, Buy oat milk, done", got)
		}
		requireRedirect(t, "marking not done", b.submit("/", "/todos/"+id+"/done", url.Values{"done": {"false"}}), "/")
		if got := f.todos(user.ID); len(got) != 1 || got[0].Done {
			t.Fatalf("to-dos after marking not done = %+v, want one, not done", got)
		}
		requireRedirect(t, "deleting", b.submit("/", "/todos/"+id+"/delete", nil), "/")
		if got := f.todos(user.ID); len(got) != 0 {
			t.Fatalf("to-dos after deleting = %+v, want none", got)
		}

		token := b.cookie(web.SessionCookieName)
		requireRedirect(t, "signing out", b.submit("/", "/signout", nil), "/signin")
		if b.cookie(web.SessionCookieName) != "" {
			t.Error("the session cookie is still set after signing out")
		}
		if _, _, err := f.base.ValidateSession(t.Context(), token); !errors.Is(err, sulis.ErrSessionNotFound) {
			t.Errorf("ValidateSession after signing out = %v, want ErrSessionNotFound", err)
		}
		requireRedirect(t, "GET / after signing out", b.get("/"), "/signin")
	})
}

func TestSignInWrongPassword(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		f.signUp("alice@example.com", "Alice")

		for _, tc := range []struct{ name, email, password string }{
			{"a wrong password", "alice@example.com", "not the password at all"},
			{"an unknown email", "nobody@example.com", password},
			{"a malformed email", "not an email", password},
		} {
			b := f.browser()
			res := b.submit("/signin", "/signin", url.Values{"email": {tc.email}, "password": {tc.password}})
			requireStatus(t, "signing in with "+tc.name, res, http.StatusUnprocessableEntity)
			requireContains(t, "the sign-in page", res.body, "Wrong email or password.")
			if b.cookie(web.SessionCookieName) != "" {
				t.Errorf("signing in with %s set a session cookie", tc.name)
			}
		}

		b := f.browser()
		b.signIn("alice@example.com")
		requireStatus(t, "GET / after signing in", b.get("/"), http.StatusOK)
	})
}

// refuseAll is a rate limiter that refuses every attempt.
type refuseAll struct{}

func (refuseAll) Allow(context.Context, string) error { return errors.New("test: refused") }

func TestSignInRateLimited(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db, sulis.WithLimiter(refuseAll{}))
		f.signUp("alice@example.com", "Alice")

		b := f.browser()
		res := b.submit("/signin", "/signin", url.Values{"email": {"alice@example.com"}, "password": {password}})
		requireStatus(t, "signing in when rate limited", res, http.StatusTooManyRequests)
		requireContains(t, "the sign-in page", res.body, "Too many attempts. Try again later.")
		if b.cookie(web.SessionCookieName) != "" {
			t.Error("a rate-limited sign-in set a session cookie")
		}
	})
}

func TestSignUpRateLimited(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db, sulis.WithLimiter(refuseAll{}))
		b := f.browser()
		res := b.submit("/signup", "/signup", url.Values{"email": {"alice@example.com"}, "password": {password}, "name": {"Alice"}})
		requireStatus(t, "signing up when rate limited", res, http.StatusTooManyRequests)
		for _, want := range []string{"Too many attempts. Try again later.", `value="alice@example.com"`, `value="Alice"`} {
			requireContains(t, "the sign-up page", res.body, want)
		}
		if b.cookie(web.SessionCookieName) != "" {
			t.Error("a rate-limited sign-up set a session cookie")
		}
		if _, err := bunstore.NewUserStore(db).GetUserByEmail(t.Context(), "alice@example.com"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Errorf("GetUserByEmail after a rate-limited sign-up = %v, want ErrUserNotFound", err)
		}
	})
}

func TestSignUpMessages(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		f.signUp("taken@example.com", "Someone")

		for _, tc := range []struct{ name, email, password, person, want string }{
			{"a taken email", "taken@example.com", password, "Alice", "An account with this email address already exists."},
			{"a bad email", "not an email", password, "Alice", "Enter a valid email address."},
			{"a short password", "alice@example.com", "short", "Alice", "Your password is too short. Use at least 12 characters."},
			{"a long password", "alice@example.com", strings.Repeat("a", 1025), "Alice", "Your password is too long."},
			{"a breached password", "alice@example.com", "Unbelievable", "Alice", "This password is on a list of common or leaked passwords. Choose another one."},
			{"an empty name", "alice@example.com", password, "   ", "Enter a name of 1 to 100 characters."},
			{"a long name", "alice@example.com", password, strings.Repeat("é", 101), "Enter a name of 1 to 100 characters."},
		} {
			b := f.browser()
			res := b.submit("/signup", "/signup", url.Values{"email": {tc.email}, "password": {tc.password}, "name": {tc.person}})
			requireStatus(t, "signing up with "+tc.name, res, http.StatusUnprocessableEntity)
			requireContains(t, "the sign-up page for "+tc.name, res.body, tc.want)
			if b.cookie(web.SessionCookieName) != "" {
				t.Errorf("signing up with %s set a session cookie", tc.name)
			}
		}
		if _, err := bunstore.NewUserStore(db).GetUserByEmail(t.Context(), "alice@example.com"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Errorf("GetUserByEmail after refused sign-ups = %v, want ErrUserNotFound", err)
		}
	})
}

func TestSignedOutGoesToSignIn(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		requireRedirect(t, "GET / with no cookie", b.get("/"), "/signin")
		requireRedirect(t, "GET an edit page with no cookie", b.get("/todos/"+uuid.NewV7().String()+"/edit"), "/signin")

		token := b.token("/signin")
		res := b.post("/todos", url.Values{"csrf_token": {token}, "text": {"signed out"}})
		requireRedirect(t, "POST /todos with no cookie", res, "/signin")

		u, err := url.Parse(f.srv.URL)
		if err != nil {
			t.Fatalf("parsing the server URL: %v", err)
		}
		b.client.Jar.SetCookies(u, []*http.Cookie{{Name: web.SessionCookieName, Value: strings.Repeat("ab", 32), Path: "/", Secure: true}})
		requireRedirect(t, "GET / with a cookie for no session", b.get("/"), "/signin")
	})
}

func TestRevokedSessionGoesToSignIn(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		f.signUp("alice@example.com", "Alice")
		first, second := f.browser(), f.browser()
		first.signIn("alice@example.com")
		second.signIn("alice@example.com")

		session, user := second.session()
		if err := f.svc.SignOut(t.Context(), user.ID, session.ID); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		requireRedirect(t, "GET / with the revoked session", second.get("/"), "/signin")
		requireStatus(t, "GET / with the other session", first.get("/"), http.StatusOK)
	})
}

func TestClientIPComesFromTheConnection(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		f.signUp("alice@example.com", "Alice")
		b := f.browser()
		form := url.Values{"csrf_token": {b.token("/signin")}, "email": {"alice@example.com"}, "password": {password}}
		header := http.Header{
			"Content-Type":    {"application/x-www-form-urlencoded"},
			"Origin":          {f.srv.URL},
			"Sec-Fetch-Site":  {"same-origin"},
			"X-Forwarded-For": {"203.0.113.9"},
			"X-Real-Ip":       {"203.0.113.9"},
		}
		requireRedirect(t, "signing in", b.do(http.MethodPost, "/signin", strings.NewReader(form.Encode()), header), "/")
		if session, _ := b.session(); session.IP != "127.0.0.1" {
			t.Errorf("the session's IP = %q, want the connection's, 127.0.0.1", session.IP)
		}
	})
}

func TestPostWithoutCSRFTokenIsRefused(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()

		for name, form := range map[string]url.Values{
			"no token":    {"text": {"no token"}},
			"a wrong one": {"text": {"wrong token"}, "csrf_token": {strings.Repeat("0", 64)}},
		} {
			requireStatus(t, "POST /todos with "+name, b.post("/todos", form), http.StatusForbidden)
		}
		if got := f.todos(user.ID); len(got) != 0 {
			t.Errorf("to-dos after refused posts = %+v, want none", got)
		}

		// Sign-up and sign-in are checked too, before anyone has a cookie.
		fresh := f.browser()
		form := url.Values{"email": {"bob@example.com"}, "password": {password}, "name": {"Bob"}}
		requireStatus(t, "POST /signup with no token", fresh.post("/signup", form), http.StatusForbidden)
		form = url.Values{"email": {"alice@example.com"}, "password": {password}}
		requireStatus(t, "POST /signin with no token", fresh.post("/signin", form), http.StatusForbidden)
		if fresh.cookie(web.SessionCookieName) != "" {
			t.Error("a refused post set a session cookie")
		}
		if _, err := bunstore.NewUserStore(db).GetUserByEmail(t.Context(), "bob@example.com"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Errorf("GetUserByEmail after a refused sign-up = %v, want ErrUserNotFound", err)
		}
	})
}

func TestCrossOriginPostIsRefused(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()
		token := b.token("/")

		for _, site := range []string{"", "cross-site"} {
			res := b.postFrom("https://evil.example", site, "/todos", url.Values{"csrf_token": {token}, "text": {"from evil"}})
			requireStatus(t, "a cross-origin POST with Sec-Fetch-Site "+site, res, http.StatusForbidden)
		}
		if got := f.todos(user.ID); len(got) != 0 {
			t.Errorf("to-dos after cross-origin posts = %+v, want none", got)
		}
		requireRedirect(t, "the same POST from the server's origin", b.post("/todos", url.Values{"csrf_token": {token}, "text": {"from here"}}), "/")
	})
}

func TestFormFromAnEarlierTabStillWorks(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()

		first := b.token("/")
		second := b.token("/")
		requireRedirect(t, "posting the first tab's form", b.post("/todos", url.Values{"csrf_token": {first}, "text": {"from the first tab"}}), "/")
		requireRedirect(t, "posting the second tab's form", b.post("/todos", url.Values{"csrf_token": {second}, "text": {"from the second tab"}}), "/")
		if got := f.todos(user.ID); len(got) != 2 {
			t.Errorf("to-dos = %+v, want two", got)
		}
	})
}

func TestOtherUsersTodoIsNotFound(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		alice := f.signUp("alice@example.com", "Alice")
		f.add(alice, "the plan for Tuesday")
		item := f.todos(alice)[0]
		id := item.ID.String()

		bob := f.browser()
		bob.signUp("bob@example.com", "Bob")
		page := bob.get("/todos/" + id + "/edit")
		requireStatus(t, "bob's GET of alice's edit page", page, http.StatusNotFound)
		if strings.Contains(page.body, item.Text) {
			t.Errorf("bob's 404 page shows alice's to-do:\n%s", page.body)
		}
		if missing := bob.get("/todos/" + uuid.NewV7().String() + "/edit"); missing.body != page.body {
			t.Errorf("alice's to-do and a missing one give different pages:\n%s\n%s", page.body, missing.body)
		}

		token := bob.token("/")
		for path, form := range map[string]url.Values{
			"/todos/" + id:             {"text": {"changed by bob"}},
			"/todos/" + id + "/done":   {"done": {"true"}},
			"/todos/" + id + "/delete": {},
		} {
			requireStatus(t, "bob's POST "+path, bob.post(path, withToken(form, token)), http.StatusNotFound)
		}
		got := f.todos(alice)
		if len(got) != 1 || got[0].Text != item.Text || got[0].Done || !got[0].UpdatedAt.Equal(item.UpdatedAt) {
			t.Errorf("alice's to-dos = %+v, want %+v unchanged", got, item)
		}
	})
}

func TestMalformedTodoIDIsNotFound(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		token := b.token("/")

		for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
			requireStatus(t, "GET /todos/"+id+"/edit", b.get("/todos/"+id+"/edit"), http.StatusNotFound)
			for _, suffix := range []string{"", "/done", "/delete"} {
				form := url.Values{"csrf_token": {token}, "text": {"x"}, "done": {"true"}}
				requireStatus(t, "POST /todos/"+id+suffix, b.post("/todos/"+id+suffix, form), http.StatusNotFound)
			}
		}
	})
}

func TestListQueryParameters(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()
		f.add(user.ID, "banana")
		f.add(user.ID, "apple")

		for _, tc := range []struct {
			path string
			want []string
		}{
			{"/", []string{"banana", "apple"}},
			{"/?sort=nonsense", []string{"banana", "apple"}},
			{"/?sort=text", []string{"apple", "banana"}},
			{"/?q=BAN", []string{"banana"}},
			{"/?q=" + strings.Repeat("x", 10_000), nil},
		} {
			page := b.get(tc.path)
			requireStatus(t, "GET "+tc.path, page, http.StatusOK)
			if got := listed(page.body); !slices.Equal(got, tc.want) {
				t.Errorf("GET %.40s lists %q, want %q", tc.path, got, tc.want)
			}
		}
	})
}

func TestSecurityHeaders(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		check := func(what string, res response, html bool) {
			t.Helper()
			want := map[string]string{"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"}
			if html {
				want["Cache-Control"] = "no-store"
			}
			for name, value := range want {
				if got := res.header.Values(name); !slices.Equal(got, []string{value}) {
					t.Errorf("%s: %s = %q, want %q", what, name, got, value)
				}
			}
		}

		res := b.get("/signin")
		requireStatus(t, "GET /signin", res, http.StatusOK)
		check("GET /signin", res, true)

		b.signUp("alice@example.com", "Alice")
		res = b.get("/")
		requireStatus(t, "GET / signed in", res, http.StatusOK)
		check("GET / signed in", res, true)

		res = b.get("/does-not-exist")
		requireStatus(t, "GET /does-not-exist", res, http.StatusNotFound)
		check("GET /does-not-exist", res, true)

		res = b.post("/todos", url.Values{"text": {"no token"}})
		requireStatus(t, "POST /todos with no token", res, http.StatusForbidden)
		check("POST /todos with no token", res, false)

		res = b.get("/static/style.css")
		requireStatus(t, "GET /static/style.css", res, http.StatusOK)
		check("GET /static/style.css", res, false)
		if got := res.header.Get("Content-Type"); got != "text/css; charset=utf-8" {
			t.Errorf("the stylesheet's Content-Type = %q, want text/css; charset=utf-8", got)
		}
	})
}

func TestOutputIsEscaped(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		const script = "<script>alert(1)</script>"
		const escaped = "&lt;script&gt;alert(1)&lt;/script&gt;"
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", script)
		_, user := b.session()
		requireRedirect(t, "adding", b.submit("/", "/todos", url.Values{"text": {script}}), "/")
		id := f.todos(user.ID)[0].ID.String()

		refused := b.submit("/", "/todos", url.Values{"text": {script + strings.Repeat("a", 500)}})
		requireStatus(t, "adding a to-do that is too long", refused, http.StatusUnprocessableEntity)
		signUp := f.browser().submit("/signup", "/signup", url.Values{"email": {"bob@example.com"}, "password": {"short"}, "name": {script}})
		requireStatus(t, "signing up with a short password", signUp, http.StatusUnprocessableEntity)

		for what, body := range map[string]string{
			"the list":                 b.get("/").body,
			"the edit page":            b.get("/todos/" + id + "/edit").body,
			"the refused list":         refused.body,
			"the refused sign-up page": signUp.body,
		} {
			requireContains(t, what, body, escaped)
			if strings.Contains(body, "<script") {
				t.Errorf("%s contains a raw <script>:\n%s", what, body)
			}
		}
	})
}

func TestBadInputIsRefused(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		const textMessage = "A to-do must be 1 to 500 characters."
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()

		for name, text := range map[string]string{"an empty to-do": "   ", "a long to-do": strings.Repeat("é", 501)} {
			res := b.submit("/", "/todos", url.Values{"text": {text}})
			requireStatus(t, "adding "+name, res, http.StatusUnprocessableEntity)
			requireContains(t, "the list after adding "+name, res.body, textMessage)
			requireContains(t, "the list after adding "+name, res.body, `action="/todos"`)
		}

		item := f.add(user.ID, "keep me")
		id := item.ID.String()
		res := b.submit("/todos/"+id+"/edit", "/todos/"+id, url.Values{"text": {""}})
		requireStatus(t, "editing to an empty to-do", res, http.StatusUnprocessableEntity)
		requireContains(t, "the edit page", res.body, textMessage)
		requireContains(t, "the edit page", res.body, `action="/todos/`+id+`"`)
		res = b.submit("/", "/todos/"+id+"/done", url.Values{"done": {"yes"}})
		requireStatus(t, "marking done with done=yes", res, http.StatusBadRequest)
		if got := f.todos(user.ID); len(got) != 1 || got[0].Text != "keep me" || got[0].Done {
			t.Errorf("to-dos = %+v, want keep me, not done", got)
		}

		for n := len(f.todos(user.ID)); n < todo.MaxTodos; n++ {
			f.add(user.ID, "filler")
		}
		res = b.submit("/", "/todos", url.Values{"text": {"one too many"}})
		requireStatus(t, "adding one too many", res, http.StatusUnprocessableEntity)
		requireContains(t, "the list after adding one too many", res.body, "You have 500 to-dos, which is the most allowed. Delete one to add another.")
		if n := len(f.todos(user.ID)); n != todo.MaxTodos {
			t.Errorf("%d to-dos, want %d", n, todo.MaxTodos)
		}
	})
}

func TestOversizedBodyIsRefused(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		_, user := b.session()

		// The text is valid, so only the body's size can refuse it.
		form := url.Values{"csrf_token": {b.token("/")}, "text": {"a to-do"}, "padding": {strings.Repeat("a", 65<<10)}}
		res := b.post("/todos", form)
		if res.status < 400 || res.status > 499 {
			t.Errorf("a 65 KiB POST /todos = %d, want a 4xx status", res.status)
		}
		if got := f.todos(user.ID); len(got) != 0 {
			t.Errorf("%d to-dos after an oversized post, want none", len(got))
		}
	})
}

func TestInternalErrorIsGeneric(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := setup(t, db)
		b := f.browser()
		b.signUp("alice@example.com", "Alice")
		token := b.cookie(web.SessionCookieName)

		if err := db.Close(); err != nil {
			t.Fatalf("closing the database: %v", err)
		}
		_, _, cause := f.base.ValidateSession(t.Context(), token)
		if cause == nil {
			t.Fatal("ValidateSession on a closed database succeeded")
		}

		res := b.get("/?q=look-for-this")
		requireStatus(t, "GET / on a closed database", res, http.StatusInternalServerError)
		requireContains(t, "the error page", res.body, "Something went wrong.")
		if strings.Contains(res.body, cause.Error()) || strings.Contains(res.body, "database is closed") {
			t.Errorf("the error page shows the error:\n%s", res.body)
		}

		logs := f.logs.String()
		for _, want := range []string{"method=GET", "path=/", "database is closed"} {
			requireContains(t, "the log", logs, want)
		}
		if strings.Contains(logs, "look-for-this") {
			t.Errorf("the log holds the query string:\n%s", logs)
		}
	})
}

func TestLogsHoldNoSecrets(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		const (
			name   = "Wilhelmina Quaxworth"
			text   = "Collect the parcel from Gwendolyn"
			query  = "parcel-filter-word"
			wrong  = "a-wrong-password-4417"
			forged = "forged-token-5528"
		)
		ctx := t.Context()
		f := setup(t, db)
		b := f.browser()
		b.signUp("wilhelmina@example.com", name)
		secrets := []string{password, wrong, name, text, query, forged, f.spec, b.cookie(web.SessionCookieName)}

		requireRedirect(t, "adding", b.submit("/", "/todos", url.Values{"text": {text}}), "/")
		requireStatus(t, "filtering", b.get("/?q="+query), http.StatusOK)
		requireStatus(t, "a forged post", b.post("/todos", url.Values{"csrf_token": {forged}, "text": {text}}), http.StatusForbidden)
		secrets = append(secrets, b.token("/"))
		requireRedirect(t, "signing out", b.submit("/", "/signout", nil), "/signin")

		res := b.submit("/signin", "/signin", url.Values{"email": {"wilhelmina@example.com"}, "password": {wrong}})
		requireStatus(t, "signing in with a wrong password", res, http.StatusUnprocessableEntity)
		b.signIn("wilhelmina@example.com")
		secrets = append(secrets, b.cookie(web.SessionCookieName))

		for _, column := range []string{"SELECT text FROM todos", "SELECT name FROM profiles"} {
			var sealed []byte
			if err := db.QueryRowContext(ctx, column).Scan(&sealed); err != nil {
				t.Fatalf("%s: %v", column, err)
			}
			secrets = append(secrets, hex.EncodeToString(sealed))
		}

		// An internal error is logged too, for a request with a query string.
		if err := db.Close(); err != nil {
			t.Fatalf("closing the database: %v", err)
		}
		requireStatus(t, "GET / on a closed database", b.get("/?q="+query), http.StatusInternalServerError)

		logs := f.logs.String()
		for _, want := range []string{"sulis security event", "kind=login.failed", "kind=csrf.rejected", "database is closed"} {
			requireContains(t, "the log", logs, want)
		}
		for _, secret := range secrets {
			if secret == "" {
				t.Fatal("a secret to look for is empty")
			}
			if strings.Contains(logs, secret) {
				t.Errorf("the log contains %.40q:\n%s", secret, logs)
			}
		}
	})
}
