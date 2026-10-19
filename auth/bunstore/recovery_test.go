package bunstore_test

import (
	"slices"
	"sync"
	"testing"
	"uuid"

	"github.com/borfast/sulis/recovery"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestRecoveryStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunRecoveryStore(t, func() recovery.Store {
			testdb.Reset(t, db)
			return bunstore.NewRecoveryStore(db)
		})
	})
}

func countRows(t *testing.T, db bun.IDB, table string, userID uuid.UUID) int {
	t.Helper()

	var n int
	err := db.NewRaw("SELECT count(*) FROM "+table+" WHERE user_id = ?", bunstore.ID(userID)).Scan(t.Context(), &n)
	if err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}

	return n
}

// SQLite allows one writer at a time, so only the Postgres subtest can show the
// race the lock row prevents.
func TestConcurrentReplaceCodesForANewUser(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewRecoveryStore(db)
		setA := []string{"a1", "a2", "a3"}
		setB := []string{"b1", "b2", "b3"}

		for i := range 20 {
			user := uuid.NewV7()
			start := make(chan struct{})
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for j, set := range [][]string{setA, setB} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs[j] = s.ReplaceCodes(t.Context(), user, set)
				}()
			}
			close(start)
			wg.Wait()

			for _, err := range errs {
				if err != nil {
					t.Fatalf("iteration %d: ReplaceCodes: %v", i, err)
				}
			}
			var got []string
			err := db.NewRaw("SELECT code_hash FROM recovery_codes WHERE user_id = ? ORDER BY code_hash", bunstore.ID(user)).Scan(t.Context(), &got)
			if err != nil {
				t.Fatalf("listing codes: %v", err)
			}
			if !slices.Equal(got, setA) && !slices.Equal(got, setB) {
				t.Fatalf("iteration %d: codes = %v, want exactly %v or %v", i, got, setA, setB)
			}
		}
	})
}

func TestReplaceCodesWithDuplicateHashes(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewRecoveryStore(db)
		ctx := t.Context()

		if err := s.ReplaceCodes(ctx, testID("alice"), []string{"h1", "h1", "h2"}); err != nil {
			t.Fatalf("ReplaceCodes: %v", err)
		}
		if n, err := s.CountCodes(ctx, testID("alice")); err != nil || n != 2 {
			t.Fatalf("CountCodes = %d, %v; want 2, nil", n, err)
		}
		if n, err := s.ConsumeCode(ctx, testID("alice"), "h1"); err != nil || n != 1 {
			t.Fatalf("ConsumeCode = %d, %v; want 1, nil", n, err)
		}
		if n, err := s.ConsumeCode(ctx, testID("alice"), "h1"); n != 0 || err != recovery.ErrCodeNotFound {
			t.Fatalf("second ConsumeCode = %d, %v; want 0, ErrCodeNotFound", n, err)
		}
	})
}

func TestReplaceCodesWithNoCodesKeepsTheSetRow(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewRecoveryStore(db)
		ctx := t.Context()

		if err := s.ReplaceCodes(ctx, testID("alice"), nil); err != nil {
			t.Fatalf("ReplaceCodes: %v", err)
		}
		if n := countRows(t, db, "recovery_code_sets", testID("alice")); n != 1 {
			t.Fatalf("set rows = %d, want 1", n)
		}
		if n, err := s.CountCodes(ctx, testID("alice")); err != nil || n != 0 {
			t.Fatalf("CountCodes = %d, %v; want 0, nil", n, err)
		}
		if n, err := s.ConsumeCode(ctx, testID("alice"), "h1"); n != 0 || err != recovery.ErrCodeNotFound {
			t.Fatalf("ConsumeCode = %d, %v; want 0, ErrCodeNotFound", n, err)
		}
	})
}

func TestReplaceCodesWithNoCodesClearsExistingCodes(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewRecoveryStore(db)
		ctx := t.Context()

		if err := s.ReplaceCodes(ctx, testID("alice"), []string{"h1", "h2"}); err != nil {
			t.Fatalf("ReplaceCodes: %v", err)
		}
		if err := s.ReplaceCodes(ctx, testID("alice"), nil); err != nil {
			t.Fatalf("ReplaceCodes(nil): %v", err)
		}
		if n, err := s.CountCodes(ctx, testID("alice")); err != nil || n != 0 {
			t.Fatalf("CountCodes = %d, %v; want 0, nil", n, err)
		}
		if n, err := s.ConsumeCode(ctx, testID("alice"), "h1"); n != 0 || err != recovery.ErrCodeNotFound {
			t.Fatalf("ConsumeCode = %d, %v; want 0, ErrCodeNotFound", n, err)
		}
	})
}

func TestDeleteCodesRemovesTheSetRow(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		s := bunstore.NewRecoveryStore(db)
		ctx := t.Context()

		if err := s.ReplaceCodes(ctx, testID("alice"), []string{"h1", "h2"}); err != nil {
			t.Fatalf("ReplaceCodes: %v", err)
		}
		if err := s.DeleteCodes(ctx, testID("alice")); err != nil {
			t.Fatalf("DeleteCodes: %v", err)
		}
		for _, table := range []string{"recovery_code_sets", "recovery_codes"} {
			if n := countRows(t, db, table, testID("alice")); n != 0 {
				t.Fatalf("%s rows = %d, want 0", table, n)
			}
		}
	})
}

func TestRecoveryStoreInsideCallerTransaction(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		s := bunstore.NewRecoveryStore(tx)
		if err := s.ReplaceCodes(ctx, testID("alice"), []string{"h1", "h2", "h3"}); err != nil {
			t.Fatalf("ReplaceCodes: %v", err)
		}
		if n, err := s.ConsumeCode(ctx, testID("alice"), "h1"); err != nil || n != 2 {
			t.Fatalf("ConsumeCode = %d, %v; want 2, nil", n, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}

		if n, err := bunstore.NewRecoveryStore(db).CountCodes(ctx, testID("alice")); err != nil || n != 0 {
			t.Fatalf("CountCodes after rollback = %d, %v; want 0, nil", n, err)
		}
	})
}
