package bunstore_test

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

// TestUserStoreEmailUniquenessIsCaseInsensitive proves commit ca52cfc's
// case-insensitive email uniqueness end to end. Sulis's storetest
// conformance suite only ever uses identical-case duplicate emails, so it
// never exercises a cross-case pair on either the write path (CreateUser)
// or the read path (GetUserByEmail). Without this, "Someone@Example.test"
// and "someone@example.test" could silently become two different accounts.
func TestUserStoreEmailUniquenessIsCaseInsensitive(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		t.Run("CreateRejectsADifferentCasingOfAnExistingEmail", func(t *testing.T) {
			testdb.Reset(t, db)
			store := bunstore.NewUserStore(db)
			ctx := t.Context()

			email := caseTestEmail()
			first := newCaseTestUser(caseTestID("first"), email)
			if err := store.CreateUser(ctx, first); err != nil {
				t.Fatalf("CreateUser(%q): %v", email, err)
			}

			dupEmail := strings.ToLower(email)
			second := newCaseTestUser(caseTestID("second"), dupEmail)
			err := store.CreateUser(ctx, second)
			if !errors.Is(err, sulis.ErrUserAlreadyExists) {
				t.Fatalf("CreateUser(%q) after creating %q: err = %v, want ErrUserAlreadyExists",
					dupEmail, email, err)
			}
		})

		t.Run("GetUserByEmailFindsADifferentCasingOfAnExistingEmail", func(t *testing.T) {
			testdb.Reset(t, db)
			store := bunstore.NewUserStore(db)
			ctx := t.Context()

			email := caseTestEmail()
			created := newCaseTestUser(caseTestID("third"), email)
			if err := store.CreateUser(ctx, created); err != nil {
				t.Fatalf("CreateUser(%q): %v", email, err)
			}

			lookup := strings.ToUpper(email)
			got, err := store.GetUserByEmail(ctx, lookup)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q): %v", lookup, err)
			}
			if got.ID != created.ID {
				t.Fatalf("GetUserByEmail(%q) returned ID %q, want %q", lookup, got.ID, created.ID)
			}
		})
	})
}

var caseTestSeq atomic.Int64

// caseTestID returns an ID unique within this test binary run.
func caseTestID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, caseTestSeq.Add(1))
}

// caseTestEmail returns a mixed-case address at a reserved TLD (RFC 2606),
// unique within this test binary run, e.g. "Someone3@Example.test".
func caseTestEmail() string {
	return fmt.Sprintf("Someone%d@Example.test", caseTestSeq.Add(1))
}

func newCaseTestUser(id, email string) *sulis.User {
	now := time.Now().UTC().Truncate(time.Second)
	return &sulis.User{
		ID:        id,
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
