package bunstore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/borfast/sulis/passkey"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

var challengeT0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func TestChallengeStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunPasskeyChallengeStore(t, func() passkey.ChallengeStore {
			testdb.Reset(t, db)
			return bunstore.NewChallengeStore(db)
		})
	})
}

// newClockedChallengeStore returns a store whose clock reads *now.
func newClockedChallengeStore(t *testing.T, db bun.IDB, now *time.Time, opts ...bunstore.ChallengeOption) *bunstore.ChallengeStore {
	t.Helper()
	s := bunstore.NewChallengeStore(db, opts...)
	bunstore.SetChallengeClock(s, func() time.Time { return *now })

	return s
}

func TestChallengeDefaultTTL(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		testdb.Reset(t, db)
		now := challengeT0
		s := newClockedChallengeStore(t, db, &now)
		ctx := t.Context()

		if err := s.SaveChallenge(ctx, "inside", []byte("in")); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveChallenge(ctx, "boundary", []byte("edge")); err != nil {
			t.Fatal(err)
		}

		now = challengeT0.Add(5*time.Minute - time.Second)
		got, err := s.ConsumeChallenge(ctx, "inside")
		if err != nil || string(got) != "in" {
			t.Fatalf("inside TTL: got %q, %v", got, err)
		}

		now = challengeT0.Add(5 * time.Minute)
		if _, err := s.ConsumeChallenge(ctx, "boundary"); !errors.Is(err, bunstore.ErrChallengeNotFound) {
			t.Fatalf("at expiry: got %v, want ErrChallengeNotFound", err)
		}
	})
}

func TestWithChallengeTTL(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		testdb.Reset(t, db)
		now := challengeT0
		s := newClockedChallengeStore(t, db, &now, bunstore.WithChallengeTTL(time.Minute))
		ctx := t.Context()

		if err := s.SaveChallenge(ctx, "k", []byte("v")); err != nil {
			t.Fatal(err)
		}
		now = challengeT0.Add(time.Minute)
		if _, err := s.ConsumeChallenge(ctx, "k"); !errors.Is(err, bunstore.ErrChallengeNotFound) {
			t.Fatalf("got %v, want ErrChallengeNotFound", err)
		}
	})
}

func TestWithChallengeTTLPanicsOnNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		func() {
			defer func() {
				want := "bunstore: challenge TTL must be positive"
				if r := recover(); r != want {
					t.Fatalf("ttl %v: recovered %v, want %q", d, r, want)
				}
			}()
			bunstore.WithChallengeTTL(d)
			t.Fatalf("ttl %v: no panic", d)
		}()
	}
}

func TestDeleteExpiredChallenges(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		testdb.Reset(t, db)
		now := challengeT0
		s := newClockedChallengeStore(t, db, &now)
		ctx := t.Context()

		for _, k := range []string{"old1", "old2"} {
			if err := s.SaveChallenge(ctx, k, []byte(k)); err != nil {
				t.Fatal(err)
			}
		}
		now = challengeT0.Add(3 * time.Minute)
		if err := s.SaveChallenge(ctx, "new", []byte("new")); err != nil {
			t.Fatal(err)
		}

		now = challengeT0.Add(5 * time.Minute)
		n, err := s.DeleteExpiredChallenges(ctx)
		if err != nil || n != 2 {
			t.Fatalf("got %d, %v, want 2, nil", n, err)
		}
		if got, err := s.ConsumeChallenge(ctx, "new"); err != nil || string(got) != "new" {
			t.Fatalf("new: got %q, %v", got, err)
		}
		if _, err := s.ConsumeChallenge(ctx, "old1"); !errors.Is(err, bunstore.ErrChallengeNotFound) {
			t.Fatalf("old1: got %v, want ErrChallengeNotFound", err)
		}
	})
}

func TestChallengeStoreInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		testdb.Reset(t, db)
		ctx := t.Context()
		s := bunstore.NewChallengeStore(db)

		if err := s.SaveChallenge(ctx, "k", []byte("v")); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bunstore.NewChallengeStore(tx).ConsumeChallenge(ctx, "k"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if got, err := s.ConsumeChallenge(ctx, "k"); err != nil || string(got) != "v" {
			t.Fatalf("after rollback: got %q, %v", got, err)
		}

		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := bunstore.NewChallengeStore(tx).SaveChallenge(ctx, "j", []byte("w")); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ConsumeChallenge(ctx, "j"); !errors.Is(err, bunstore.ErrChallengeNotFound) {
			t.Fatalf("j: got %v, want ErrChallengeNotFound", err)
		}
	})
}
