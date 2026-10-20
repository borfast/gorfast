package storage

import (
	"fmt"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
)

// Binder returns the bind function for data.NewBunRunner: every store reads
// and writes through tx, and Auth is a copy of base that shares its settings.
func Binder(base *sulis.Sulis, enc crypt.Encryptor) func(tx bun.IDB) (todo.Repos, error) {
	return func(tx bun.IDB) (todo.Repos, error) {
		auth, err := base.WithStores(bunstore.NewUserStore(tx), bunstore.NewSessionStore(tx), bunstore.NewTokenStore(tx), sulis.NoSecondFactors{})
		if err != nil {
			return todo.Repos{}, fmt.Errorf("storage: binding the auth stores: %w", err)
		}
		return todo.Repos{
			Auth:     auth,
			Profiles: NewProfileStore(tx, enc),
			Todos:    NewTodoStore(tx, enc),
		}, nil
	}
}
