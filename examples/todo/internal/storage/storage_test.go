package storage_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/examples/todo/internal/storage"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
	"github.com/borfast/gorfast/internal/testdb"
)

var errFail = errors.New("test: fail")

// setup applies this package's migrations and returns a keyring with a new key.
func setup(t *testing.T, db *bun.DB) crypt.Encryptor {
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
	return kr
}

func newTodo(owner uuid.UUID, text string, at time.Time) todo.Todo {
	return todo.Todo{ID: uuid.NewV7(), OwnerID: owner, Text: text, CreatedAt: at, UpdatedAt: at}
}

func create(t *testing.T, s *storage.TodoStore, items ...todo.Todo) {
	t.Helper()
	for _, item := range items {
		if err := s.CreateTodo(t.Context(), item); err != nil {
			t.Fatalf("CreateTodo %q: %v", item.Text, err)
		}
	}
}

func requireTodo(t *testing.T, got, want todo.Todo) {
	t.Helper()
	if got.ID != want.ID || got.OwnerID != want.OwnerID || got.Text != want.Text || got.Done != want.Done {
		t.Errorf("to-do = %+v, want %+v", got, want)
	}
	for _, pair := range [][2]time.Time{{got.CreatedAt, want.CreatedAt}, {got.UpdatedAt, want.UpdatedAt}} {
		if !pair[0].Truncate(time.Microsecond).Equal(pair[1].Truncate(time.Microsecond)) {
			t.Errorf("time = %v, want %v", pair[0], pair[1])
		}
		if pair[0].Location() != time.UTC {
			t.Errorf("time %v is not in UTC", pair[0])
		}
	}
}

func requireTodos(t *testing.T, got []todo.Todo, want ...todo.Todo) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d to-dos, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		requireTodo(t, got[i], want[i])
	}
}

func requireOpenFailure(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || errors.Is(err, todo.ErrNotFound) || !errors.Is(err, crypt.ErrCannotOpen) {
		t.Errorf("%s = %v, want a wrapped crypt.ErrCannotOpen", what, err)
	}
}

func exec(t *testing.T, db *bun.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.NewRaw(query, args...).Exec(t.Context()); err != nil {
		t.Fatalf("%s: %v", query, err)
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

func TestApplyTwice(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		setup(t, db)
		if err := storage.Apply(t.Context(), db); err != nil {
			t.Fatalf("second Apply: %v", err)
		}
		if n := count(t, db, "profiles") + count(t, db, "todos"); n != 0 {
			t.Errorf("%d rows after Apply, want 0", n)
		}
	})
}

func TestTodoStoreRoundTrip(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := storage.NewTodoStore(db, setup(t, db))
		alice := uuid.NewV7()
		now := time.Now().UTC()
		first := newTodo(alice, "buy milk", now)
		second := newTodo(alice, "walk the dog, 🐕", now.Add(time.Second))
		second.Done = true
		create(t, s, first, second)

		for _, want := range []todo.Todo{first, second} {
			got, err := s.GetTodo(ctx, alice, want.ID)
			if err != nil {
				t.Fatalf("GetTodo %q: %v", want.Text, err)
			}
			requireTodo(t, got, want)
		}
		list, err := s.ListTodos(ctx, alice)
		if err != nil {
			t.Fatalf("ListTodos: %v", err)
		}
		requireTodos(t, list, first, second)

		// CreatedAt is changed here to check that UpdateTodo does not write it.
		changed := first
		changed.Text, changed.Done = "buy oat milk", true
		changed.UpdatedAt, changed.CreatedAt = now.Add(time.Minute), now.Add(time.Hour)
		if err := s.UpdateTodo(ctx, changed); err != nil {
			t.Fatalf("UpdateTodo: %v", err)
		}
		got, err := s.GetTodo(ctx, alice, first.ID)
		if err != nil {
			t.Fatalf("GetTodo after update: %v", err)
		}
		changed.CreatedAt = first.CreatedAt
		requireTodo(t, got, changed)

		if err := s.DeleteTodo(ctx, alice, second.ID); err != nil {
			t.Fatalf("DeleteTodo: %v", err)
		}
		if _, err := s.GetTodo(ctx, alice, second.ID); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("GetTodo after delete = %v, want ErrNotFound", err)
		}
		if n, err := s.CountTodos(ctx, alice); err != nil || n != 1 {
			t.Errorf("CountTodos = %d, %v; want 1", n, err)
		}
	})
}

func TestTodoStoreScopesByOwner(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := storage.NewTodoStore(db, setup(t, db))
		alice, bob := uuid.NewV7(), uuid.NewV7()
		item := newTodo(alice, "alice's plans", time.Now().UTC())
		create(t, s, item)

		if _, err := s.GetTodo(ctx, bob, item.ID); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's GetTodo = %v, want ErrNotFound", err)
		}
		taken := item
		taken.OwnerID, taken.Text, taken.Done = bob, "bob was here", true
		if err := s.UpdateTodo(ctx, taken); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's UpdateTodo = %v, want ErrNotFound", err)
		}
		if err := s.DeleteTodo(ctx, bob, item.ID); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("bob's DeleteTodo = %v, want ErrNotFound", err)
		}
		if list, err := s.ListTodos(ctx, bob); err != nil || len(list) != 0 {
			t.Errorf("bob's ListTodos = %+v, %v; want none", list, err)
		}
		if n, err := s.CountTodos(ctx, bob); err != nil || n != 0 {
			t.Errorf("bob's CountTodos = %d, %v; want 0", n, err)
		}

		got, err := s.GetTodo(ctx, alice, item.ID)
		if err != nil {
			t.Fatalf("alice's GetTodo: %v", err)
		}
		requireTodo(t, got, item)
	})
}

func TestProfileStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		s := storage.NewProfileStore(db, setup(t, db))
		alice := todo.Profile{UserID: uuid.NewV7(), Name: "Alice Liddell"}
		if err := s.CreateProfile(ctx, alice); err != nil {
			t.Fatalf("CreateProfile: %v", err)
		}
		if got, err := s.GetProfile(ctx, alice.UserID); err != nil || got != alice {
			t.Errorf("GetProfile = %+v, %v; want %+v", got, err, alice)
		}
		if err := s.LockProfile(ctx, alice.UserID); err != nil {
			t.Errorf("LockProfile = %v, want nil", err)
		}

		unknown := uuid.NewV7()
		if _, err := s.GetProfile(ctx, unknown); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("GetProfile of an unknown user = %v, want ErrNotFound", err)
		}
		if err := s.LockProfile(ctx, unknown); !errors.Is(err, todo.ErrNotFound) {
			t.Errorf("LockProfile of an unknown user = %v, want ErrNotFound", err)
		}

		again := todo.Profile{UserID: alice.UserID, Name: "Someone Else"}
		if err := s.CreateProfile(ctx, again); err == nil {
			t.Error("second CreateProfile for the same user succeeded")
		}
		if got, err := s.GetProfile(ctx, alice.UserID); err != nil || got != alice {
			t.Errorf("GetProfile after the second create = %+v, %v; want %+v", got, err, alice)
		}
	})
}

func TestStoredValuesAreNotPlaintext(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		enc := setup(t, db)
		alice := uuid.NewV7()
		name, text := "Alice Liddell", "the plaintext of a to-do"
		if err := storage.NewProfileStore(db, enc).CreateProfile(ctx, todo.Profile{UserID: alice, Name: name}); err != nil {
			t.Fatalf("CreateProfile: %v", err)
		}
		create(t, storage.NewTodoStore(db, enc), newTodo(alice, text, time.Now().UTC()))

		for _, c := range []struct{ query, plaintext string }{
			{"SELECT name FROM profiles", name},
			{"SELECT text FROM todos", text},
		} {
			var raw []byte
			if err := db.NewRaw(c.query).Scan(ctx, &raw); err != nil {
				t.Fatalf("%s: %v", c.query, err)
			}
			if len(raw) <= len(c.plaintext) {
				t.Errorf("%s: %d bytes, want ciphertext longer than the plaintext", c.query, len(raw))
			}
			if bytes.Contains(raw, []byte(c.plaintext)) {
				t.Errorf("%s: the stored value contains the plaintext", c.query)
			}
		}
	})
}

func TestSealedValueMovedElsewhereFailsToOpen(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		enc := setup(t, db)
		todos := storage.NewTodoStore(db, enc)
		alice, bob := uuid.NewV7(), uuid.NewV7()
		now := time.Now().UTC()
		alicesTodo, alicesOther, bobsTodo := newTodo(alice, "alice's", now), newTodo(alice, "alice's other", now), newTodo(bob, "bob's", now)
		moved := newTodo(alice, "moved to bob", now)
		create(t, todos, alicesTodo, alicesOther, bobsTodo, moved)

		copyText := "UPDATE todos SET text = (SELECT text FROM todos WHERE id = ?) WHERE id = ?"
		exec(t, db, copyText, bunstore.ID(alicesTodo.ID), bunstore.ID(bobsTodo.ID))
		_, err := todos.GetTodo(ctx, bob, bobsTodo.ID)
		requireOpenFailure(t, "GetTodo of text copied to another user", err)
		_, err = todos.ListTodos(ctx, bob)
		requireOpenFailure(t, "ListTodos with text copied to another user", err)

		exec(t, db, copyText, bunstore.ID(alicesTodo.ID), bunstore.ID(alicesOther.ID))
		_, err = todos.GetTodo(ctx, alice, alicesOther.ID)
		requireOpenFailure(t, "GetTodo of text copied to another of the owner's rows", err)

		exec(t, db, "UPDATE todos SET owner_id = ? WHERE id = ?", bunstore.ID(bob), bunstore.ID(moved.ID))
		_, err = todos.GetTodo(ctx, bob, moved.ID)
		requireOpenFailure(t, "GetTodo of a row moved to another owner", err)

		profiles := storage.NewProfileStore(db, enc)
		for _, p := range []todo.Profile{{UserID: alice, Name: "Alice"}, {UserID: bob, Name: "Bob"}} {
			if err := profiles.CreateProfile(ctx, p); err != nil {
				t.Fatalf("CreateProfile %s: %v", p.Name, err)
			}
		}
		exec(t, db, "UPDATE profiles SET name = (SELECT name FROM profiles WHERE user_id = ?) WHERE user_id = ?", bunstore.ID(alice), bunstore.ID(bob))
		_, err = profiles.GetProfile(ctx, bob)
		requireOpenFailure(t, "GetProfile of a name copied to another user", err)
	})
}

func TestBinderRollsBackEverything(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		enc := setup(t, db)
		base, err := sulis.New(bunstore.NewUserStore(db), bunstore.NewSessionStore(db), bunstore.NewTokenStore(db), sulis.NoSecondFactors{},
			sulis.WithArgon2Params(sulis.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}))
		if err != nil {
			t.Fatalf("sulis.New: %v", err)
		}
		runner := data.NewBunRunner(db, storage.Binder(base, enc))

		err = runner.Run(ctx, func(ctx context.Context, repos todo.Repos) (error, error) {
			user, session, _, err := repos.Auth.Register(ctx, "alice@example.com", "correct-battery-staple", sulis.RequestInfo{})
			if err != nil {
				return nil, err
			}
			if session == nil {
				t.Error("Register returned no session")
			}
			if err := repos.Profiles.CreateProfile(ctx, todo.Profile{UserID: user.ID, Name: "Alice"}); err != nil {
				return nil, err
			}
			return nil, errFail
		})
		if !errors.Is(err, errFail) {
			t.Fatalf("Run = %v, want errFail", err)
		}

		if _, err := bunstore.NewUserStore(db).GetUserByEmail(ctx, "alice@example.com"); !errors.Is(err, sulis.ErrUserNotFound) {
			t.Errorf("GetUserByEmail after rollback = %v, want ErrUserNotFound", err)
		}
		for _, table := range []string{"sessions", "profiles"} {
			if n := count(t, db, table); n != 0 {
				t.Errorf("%d rows in %s after rollback, want 0", n, table)
			}
		}
	})
}
