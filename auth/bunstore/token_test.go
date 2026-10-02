package bunstore_test

import (
	"testing"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestTokenStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunTokenStore(t, func() sulis.TokenStore {
			testdb.Reset(t, db)
			return bunstore.NewTokenStore(db)
		})
	})
}
