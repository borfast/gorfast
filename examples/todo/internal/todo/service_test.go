package todo_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/examples/todo/internal/events"
	"github.com/borfast/gorfast/examples/todo/internal/storage"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
	"github.com/borfast/gorfast/internal/testdb"
)

const password = "correct-battery-staple"

var errFail = errors.New("test: fail")

// fixture is one database with its keyring and base Sulis.
type fixture struct {
	db   *bun.DB
	enc  crypt.Encryptor
	base *sulis.Sulis
}

// newFixture applies the migrations and builds the base Sulis, adding opts to
// the options every test uses.
func newFixture(t *testing.T, db *bun.DB, opts ...sulis.Option) fixture {
	t.Helper()
	if err := storage.Apply(t.Context(), db); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	key, err := crypt.GenerateKey(crypt.XChaCha20Poly1305)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kr, err := crypt.NewKeyring(key)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	opts = append([]sulis.Option{
		sulis.WithRequireVerifiedEmail(false),
		sulis.WithoutRateLimiting(),
		sulis.WithArgon2Params(sulis.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}),
	}, opts...)
	base, err := sulis.New(bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db), sulis.NoSecondFactors{}, opts...)
	if err != nil {
		t.Fatalf("sulis.New: %v", err)
	}
	return fixture{db: db, enc: kr, base: base}
}

func (f fixture) service(bind func(tx bun.IDB) (todo.Repos, error)) *todo.Service {
	return todo.NewService(data.NewBunRunner(f.db, bind))
}

// binder is storage.Binder with the user store and second factor checker
// replaced, for the cases storage.Binder cannot inject.
func (f fixture) binder(users func(tx bun.IDB) sulis.UserStore, factors sulis.SecondFactorChecker) func(tx bun.IDB) (todo.Repos, error) {
	return func(tx bun.IDB) (todo.Repos, error) {
		auth, err := f.base.WithStores(users(tx), bunstore.NewSessionStore(tx), bunstore.NewTokenStore(tx), factors)
		if err != nil {
			return todo.Repos{}, err
		}
		return todo.Repos{Auth: auth, Profiles: storage.NewProfileStore(tx, f.enc), Todos: storage.NewTodoStore(tx, f.enc)}, nil
	}
}

func setup(t *testing.T, db *bun.DB) (fixture, *todo.Service) {
	t.Helper()
	f := newFixture(t, db)
	return f, f.service(storage.Binder(f.base, f.enc))
}

// validate returns the session and user behind token, failing the test if
// the token is not valid.
func validate(t *testing.T, f fixture, token string) (*sulis.Session, *sulis.User) {
	t.Helper()
	session, user, err := f.base.ValidateSession(t.Context(), token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	return session, user
}

func signUp(t *testing.T, f fixture, svc *todo.Service, email string) (uuid.UUID, todo.SignedIn) {
	t.Helper()
	in, err := svc.SignUp(t.Context(), email, password, "Someone", sulis.RequestInfo{})
	if err != nil {
		t.Fatalf("SignUp %s: %v", email, err)
	}
	_, user := validate(t, f, in.Token)
	return user.ID, in
}

func add(t *testing.T, svc *todo.Service, owner uuid.UUID, text string) todo.Todo {
	t.Helper()
	item, err := svc.Add(t.Context(), owner, text)
	if err != nil {
		t.Fatalf("Add %q: %v", text, err)
	}
	return item
}

func list(t *testing.T, svc *todo.Service, owner uuid.UUID, filter string, order todo.Order) []todo.Todo {
	t.Helper()
	todos, err := svc.List(t.Context(), owner, filter, order)
	if err != nil {
		t.Fatalf("List(%q, %q): %v", filter, order, err)
	}
	return todos
}

// only returns the single to-do owner has.
func only(t *testing.T, svc *todo.Service, owner uuid.UUID) todo.Todo {
	t.Helper()
	todos := list(t, svc, owner, "", todo.ByCreated)
	if len(todos) != 1 {
		t.Fatalf("List = %+v, want one to-do", todos)
	}
	return todos[0]
}

func requireTexts(t *testing.T, got []todo.Todo, want ...string) {
	t.Helper()
	texts := make([]string, 0, len(got))
	for _, item := range got {
		texts = append(texts, item.Text)
	}
	if !slices.Equal(texts, want) {
		t.Errorf("texts = %q, want %q", texts, want)
	}
}

func count(t *testing.T, db *bun.DB, table string) int {
	t.Helper()
	var n int
	if err := db.NewRaw("SELECT count(*) FROM "+table).Scan(t.Context(), &n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

func countTodos(t *testing.T, f fixture, owner uuid.UUID) int {
	t.Helper()
	n, err := storage.NewTodoStore(f.db, f.enc).CountTodos(t.Context(), owner)
	if err != nil {
		t.Fatalf("CountTodos: %v", err)
	}
	return n
}

func storedUser(t *testing.T, f fixture, email string) *sulis.User {
	t.Helper()
	user, err := bunstore.NewUserStore(f.db).GetUserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	return user
}

type failingProfiles struct{ todo.ProfileStore }

func (failingProfiles) CreateProfile(context.Context, todo.Profile) error { return errFail }

type failingUsers struct{ *bunstore.UserStore }

func (failingUsers) UpdateUser(context.Context, *sulis.User) error { return errFail }

type alwaysSecondFactor struct{}

func (alwaysSecondFactor) HasSecondFactor(context.Context, uuid.UUID) (bool, error) { return true, nil }

func TestSignUpThenSignIn(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		up, err := svc.SignUp(ctx, "alice@example.com", password, "  Alice Liddell ", sulis.RequestInfo{})
		if err != nil {
			t.Fatalf("SignUp: %v", err)
		}
		session, user := validate(t, f, up.Token)
		if up.Expires.Location() != time.UTC || up.Expires.Sub(session.ExpiresAt).Abs() > time.Second {
			t.Errorf("Expires = %v, want the session's expiry %v in UTC", up.Expires, session.ExpiresAt)
		}
		if name, err := svc.Name(ctx, user.ID); err != nil || name != "Alice Liddell" {
			t.Errorf("Name = %q, %v; want %q", name, err, "Alice Liddell")
		}

		in, err := svc.SignIn(ctx, "alice@example.com", password, sulis.RequestInfo{})
		if err != nil {
			t.Fatalf("SignIn: %v", err)
		}
		if in.Token == up.Token {
			t.Error("SignIn returned the sign-up token, want a new session")
		}
		if _, again := validate(t, f, in.Token); again.ID != user.ID {
			t.Errorf("SignIn's session belongs to %v, want %v", again.ID, user.ID)
		}
		if in.Expires.Location() != time.UTC || !in.Expires.After(time.Now()) {
			t.Errorf("SignIn Expires = %v, want a future time in UTC", in.Expires)
		}

		for _, email := range []string{"alice@example.com", "nobody@example.com"} {
			if _, err := svc.SignIn(ctx, email, "not the password", sulis.RequestInfo{}); !errors.Is(err, sulis.ErrInvalidCredentials) {
				t.Errorf("SignIn %s with a wrong password = %v, want ErrInvalidCredentials", email, err)
			}
		}
	})
}

func TestSignUpFailureAfterRegisterLeavesNothing(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		var logs strings.Builder
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		f := newFixture(t, db, sulis.WithEventSink(events.NewSink(logger)))
		bind := storage.Binder(f.base, f.enc)
		svc := todo.NewService(events.Hold(data.NewBunRunner(db, func(tx bun.IDB) (todo.Repos, error) {
			repos, err := bind(tx)
			repos.Profiles = failingProfiles{repos.Profiles}
			return repos, err
		}), logger))

		in, err := svc.SignUp(ctx, "alice@example.com", password, "Alice", sulis.RequestInfo{})
		if !errors.Is(err, errFail) || in != (todo.SignedIn{}) {
			t.Fatalf("SignUp = %+v, %v; want nothing and errFail", in, err)
		}
		var registered []string
		for line := range strings.Lines(logs.String()) {
			if strings.Contains(line, "kind=account.registered") {
				registered = append(registered, line)
			}
		}
		if len(registered) != 1 || !strings.Contains(registered[0], "transaction=rolled_back") {
			t.Errorf("the registration event was logged as %q, want once with transaction=rolled_back", registered)
		}
		if _, err := bunstore.NewUserStore(db).GetUserByEmail(ctx, "alice@example.com"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Errorf("GetUserByEmail after a failed sign-up = %v, want ErrUserNotFound", err)
		}
		for _, table := range []string{"users", "sessions", "tokens", "profiles"} {
			if n := count(t, db, table); n != 0 {
				t.Errorf("%d rows in %s after a failed sign-up, want 0", n, table)
			}
		}
	})
}

// refuseAll is a limiter that refuses every key.
type refuseAll struct{}

func (refuseAll) Allow(context.Context, string) error { return errFail }

func TestRateLimitedSignUpLogsWithoutTheRollbackLabel(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		var logs strings.Builder
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		f := newFixture(t, db, sulis.WithLimiter(refuseAll{}), sulis.WithEventSink(events.NewSink(logger)))
		svc := todo.NewService(events.Hold(data.NewBunRunner(db, storage.Binder(f.base, f.enc)), logger))

		_, err := svc.SignUp(t.Context(), "alice@example.com", password, "Alice", sulis.RequestInfo{IP: "192.0.2.1"})
		if !errors.Is(err, sulis.ErrRateLimited) {
			t.Fatalf("SignUp = %v, want ErrRateLimited", err)
		}
		var tripped []string
		for line := range strings.Lines(logs.String()) {
			if strings.Contains(line, "kind=ratelimit.tripped") {
				tripped = append(tripped, line)
			}
		}
		if len(tripped) != 1 || strings.Contains(tripped[0], "transaction=") {
			t.Errorf("the rate limit event was logged as %q, want once without a transaction label", tripped)
		}
		if n := count(t, db, "users"); n != 0 {
			t.Errorf("%d rows in users after a rate-limited sign-up, want 0", n)
		}
	})
}

func TestSignUpChecksTheName(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		for i, name := range []string{"", "   ", strings.Repeat("é", todo.MaxNameLength+1)} {
			email := fmt.Sprintf("bad%d@example.com", i)
			if _, err := svc.SignUp(ctx, email, password, name, sulis.RequestInfo{}); !errors.Is(err, todo.ErrNameInvalid) {
				t.Errorf("SignUp with a name of %d runes = %v, want ErrNameInvalid", len([]rune(name)), err)
			}
		}
		if n := count(t, db, "users"); n != 0 {
			t.Errorf("%d users after refused sign-ups, want 0", n)
		}

		longest := strings.Repeat("é", todo.MaxNameLength)
		in, err := svc.SignUp(ctx, "good@example.com", password, longest, sulis.RequestInfo{})
		if err != nil {
			t.Fatalf("SignUp with a name of %d runes: %v", todo.MaxNameLength, err)
		}
		_, user := validate(t, f, in.Token)
		if name, err := svc.Name(ctx, user.ID); err != nil || name != longest {
			t.Errorf("Name = %q, %v; want %q", name, err, longest)
		}
	})
}

func TestSignInKeepsTheFailedAttemptCount(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		// The application leaves lockout off. It is on here so that a wrong
		// password writes something, and the test can check that it commits.
		f := newFixture(t, db, sulis.WithFailureLockout(5, time.Minute, time.Hour))
		svc := f.service(storage.Binder(f.base, f.enc))
		signUp(t, f, svc, "alice@example.com")

		if _, err := svc.SignIn(t.Context(), "alice@example.com", "not the password", sulis.RequestInfo{}); !errors.Is(err, sulis.ErrInvalidCredentials) {
			t.Fatalf("SignIn with a wrong password = %v, want ErrInvalidCredentials", err)
		}
		if n := storedUser(t, f, "alice@example.com").FailedLoginAttempts; n != 1 {
			t.Errorf("FailedLoginAttempts = %d, want 1", n)
		}
	})
}

func TestSignInStorageFailureIsNotAWrongPassword(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		// The application leaves lockout off. It is on here so that a wrong
		// password writes, and the test can make that write fail.
		f := newFixture(t, db, sulis.WithFailureLockout(5, time.Minute, time.Hour))
		signUp(t, f, f.service(storage.Binder(f.base, f.enc)), "alice@example.com")
		svc := f.service(f.binder(func(tx bun.IDB) sulis.UserStore {
			return failingUsers{bunstore.NewUserStore(tx)}
		}, sulis.NoSecondFactors{}))

		_, err := svc.SignIn(t.Context(), "alice@example.com", "not the password", sulis.RequestInfo{})
		if err == nil || errors.Is(err, sulis.ErrInvalidCredentials) || !errors.Is(err, errFail) {
			t.Errorf("SignIn with a failing user store = %v, want errFail and not ErrInvalidCredentials", err)
		}
	})
}

func TestSignInNeedingASecondFactorFails(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f := newFixture(t, db)
		signUp(t, f, f.service(storage.Binder(f.base, f.enc)), "alice@example.com")
		svc := f.service(f.binder(func(tx bun.IDB) sulis.UserStore {
			return bunstore.NewUserStore(tx)
		}, alwaysSecondFactor{}))

		in, err := svc.SignIn(t.Context(), "alice@example.com", password, sulis.RequestInfo{})
		if err == nil || in != (todo.SignedIn{}) {
			t.Errorf("SignIn needing a second factor = %+v, %v; want nothing and an error", in, err)
		}
		if n := count(t, db, "tokens"); n != 0 {
			t.Errorf("%d tokens after a refused sign-in, want 0", n)
		}
		if n := count(t, db, "sessions"); n != 1 {
			t.Errorf("%d sessions after a refused sign-in, want only the sign-up session", n)
		}
	})
}

func TestSignOut(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		_, alice := signUp(t, f, svc, "alice@example.com")
		bob, _ := signUp(t, f, svc, "bob@example.com")
		session, user := validate(t, f, alice.Token)

		if err := svc.SignOut(ctx, bob, session.ID); !errors.Is(err, sulis.ErrSessionNotFound) {
			t.Errorf("bob's SignOut of alice's session = %v, want ErrSessionNotFound", err)
		}
		validate(t, f, alice.Token)

		if err := svc.SignOut(ctx, user.ID, session.ID); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		if _, _, err := f.base.ValidateSession(ctx, alice.Token); err == nil {
			t.Error("ValidateSession after SignOut succeeded")
		}
	})
}

func TestTodosBelongToTheirOwner(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		bob, _ := signUp(t, f, svc, "bob@example.com")
		item := add(t, svc, alice, "alice's plans")
		want := only(t, svc, alice)

		if err := svc.Edit(ctx, bob, item.ID, "bob was here"); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's Edit = %v, want ErrNotFound", err)
		}
		if err := svc.SetDone(ctx, bob, item.ID, true); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's SetDone = %v, want ErrNotFound", err)
		}
		if err := svc.Delete(ctx, bob, item.ID); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's Delete = %v, want ErrNotFound", err)
		}
		if got := list(t, svc, bob, "", todo.ByCreated); len(got) != 0 {
			t.Errorf("bob's List = %+v, want none", got)
		}

		got := only(t, svc, alice)
		if got.Text != want.Text || got.Done != want.Done || !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Errorf("alice's to-do = %+v, want it unchanged: %+v", got, want)
		}
	})
}

func TestAddChecksTheText(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")

		longest := strings.Repeat("é", todo.MaxTextLength)
		item := add(t, svc, alice, " "+longest+" ")
		if item.Text != longest || item.OwnerID != alice || item.ID == uuid.Nil() {
			t.Errorf("Add = %+v, want the trimmed text owned by alice", item)
		}
		if item.CreatedAt.Location() != time.UTC || !item.UpdatedAt.Equal(item.CreatedAt) {
			t.Errorf("Add times = %v, %v; want equal UTC times", item.CreatedAt, item.UpdatedAt)
		}
		requireTexts(t, list(t, svc, alice, "", todo.ByCreated), longest)

		for _, text := range []string{strings.Repeat("é", todo.MaxTextLength+1), "", "  "} {
			if _, err := svc.Add(ctx, alice, text); !errors.Is(err, todo.ErrTextInvalid) {
				t.Errorf("Add of %d runes = %v, want ErrTextInvalid", len([]rune(text)), err)
			}
		}
		if n := countTodos(t, f, alice); n != 1 {
			t.Errorf("CountTodos = %d, want 1", n)
		}
	})
}

func TestAddStopsAtTheLimit(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		for i := range todo.MaxTodos {
			add(t, svc, alice, fmt.Sprintf("to-do %d", i))
		}

		if _, err := svc.Add(t.Context(), alice, "one too many"); !errors.Is(err, todo.ErrTooMany) {
			t.Errorf("Add past the limit = %v, want ErrTooMany", err)
		}
		if n := countTodos(t, f, alice); n != todo.MaxTodos {
			t.Errorf("CountTodos = %d, want %d", n, todo.MaxTodos)
		}
	})
}

func TestConcurrentAddsStayWithinTheLimit(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		const room = 5
		for i := range todo.MaxTodos - room {
			add(t, svc, alice, fmt.Sprintf("to-do %d", i))
		}

		errs := make([]error, 20)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() {
				<-start
				_, errs[i] = svc.Add(t.Context(), alice, fmt.Sprintf("racing %d", i))
			})
		}
		close(start)
		wg.Wait()

		added := 0
		for _, err := range errs {
			switch {
			case err == nil:
				added++
			case !errors.Is(err, todo.ErrTooMany):
				t.Errorf("concurrent Add = %v, want nil or ErrTooMany", err)
			}
		}
		if added != room {
			t.Errorf("%d concurrent Add calls succeeded, want %d", added, room)
		}
		if n := countTodos(t, f, alice); n != todo.MaxTodos {
			t.Errorf("CountTodos = %d, want %d", n, todo.MaxTodos)
		}
	})
}

func TestListSortsAndFilters(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		for _, text := range []string{"banana", "Apple", "cherry pie", "apple tart"} {
			add(t, svc, alice, text)
		}

		requireTexts(t, list(t, svc, alice, "", todo.ByCreated), "banana", "Apple", "cherry pie", "apple tart")
		requireTexts(t, list(t, svc, alice, "", todo.ByText), "Apple", "apple tart", "banana", "cherry pie")
		requireTexts(t, list(t, svc, alice, " APPLE ", todo.ByCreated), "Apple", "apple tart")
		requireTexts(t, list(t, svc, alice, "", todo.Order("nonsense")), "banana", "Apple", "cherry pie", "apple tart")
		requireTexts(t, list(t, svc, alice, "pear", todo.ByText))

		// Sorting by bytes would put "Blueberry" before "apple tart".
		add(t, svc, alice, "Blueberry")
		requireTexts(t, list(t, svc, alice, "", todo.ByText), "Apple", "apple tart", "banana", "Blueberry", "cherry pie")
	})
}

func TestEditAndSetDone(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		item := add(t, svc, alice, "buy milk")
		before := only(t, svc, alice)

		if err := svc.Edit(ctx, alice, item.ID, " buy oat milk  "); err != nil {
			t.Fatalf("Edit: %v", err)
		}
		edited := only(t, svc, alice)
		if edited.Text != "buy oat milk" || edited.Done || !edited.CreatedAt.Equal(before.CreatedAt) || !edited.UpdatedAt.After(before.UpdatedAt) {
			t.Errorf("after Edit = %+v, want the new text and a later UpdatedAt than %+v", edited, before)
		}

		if err := svc.SetDone(ctx, alice, item.ID, true); err != nil {
			t.Fatalf("SetDone: %v", err)
		}
		done := only(t, svc, alice)
		if done.Text != "buy oat milk" || !done.Done || !done.UpdatedAt.After(edited.UpdatedAt) {
			t.Errorf("after SetDone = %+v, want done and a later UpdatedAt than %+v", done, edited)
		}

		if err := svc.Edit(ctx, alice, item.ID, "   "); !errors.Is(err, todo.ErrTextInvalid) {
			t.Errorf("Edit to blank text = %v, want ErrTextInvalid", err)
		}
		if err := svc.SetDone(ctx, alice, item.ID, false); err != nil {
			t.Fatalf("SetDone false: %v", err)
		}
		if undone := only(t, svc, alice); undone.Text != "buy oat milk" || undone.Done {
			t.Errorf("after SetDone false = %+v, want not done and the text kept", undone)
		}
		if err := svc.Edit(ctx, alice, uuid.NewV7(), "no such to-do"); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("Edit of an unknown ID = %v, want ErrNotFound", err)
		}
	})
}

func TestConcurrentEditAndSetDoneKeepBoth(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		store := storage.NewTodoStore(db, f.enc)
		for round := range 50 {
			item := add(t, svc, alice, "before")
			text := fmt.Sprintf("after %d", round)
			var editErr, doneErr error
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Go(func() { <-start; editErr = svc.Edit(ctx, alice, item.ID, text) })
			wg.Go(func() { <-start; doneErr = svc.SetDone(ctx, alice, item.ID, true) })
			close(start)
			wg.Wait()
			if editErr != nil || doneErr != nil {
				t.Fatalf("round %d: Edit = %v, SetDone = %v; want both nil", round, editErr, doneErr)
			}
			got, err := store.GetTodo(ctx, alice, item.ID)
			if err != nil {
				t.Fatalf("round %d: GetTodo: %v", round, err)
			}
			if got.Text != text || !got.Done {
				t.Fatalf("round %d: to-do = %q, done %v; want %q, done true", round, got.Text, got.Done, text)
			}
		}
	})
}

func TestDelete(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		f, svc := setup(t, db)
		alice, _ := signUp(t, f, svc, "alice@example.com")
		item := add(t, svc, alice, "buy milk")

		if err := svc.Delete(ctx, alice, item.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if got := list(t, svc, alice, "", todo.ByCreated); len(got) != 0 {
			t.Errorf("List after Delete = %+v, want none", got)
		}
		if err := svc.Delete(ctx, alice, item.ID); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("second Delete = %v, want ErrNotFound", err)
		}
	})
}
