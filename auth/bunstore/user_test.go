package bunstore_test

import (
	"testing"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestUserStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunUserStore(t, func() sulis.UserStore {
			testdb.Reset(t, db)
			return bunstore.NewUserStore(db)
		})
	})
}
