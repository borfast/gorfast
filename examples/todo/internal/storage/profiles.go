package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"uuid"

	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
)

type profileModel struct {
	bun.BaseModel `bun:"table:profiles,alias:p"`

	UserID bunstore.ID  `bun:"user_id,pk,type:uuid"`
	Name   crypt.Sealed `bun:"name"`
}

// ProfileStore implements todo.ProfileStore, sealing each name to its user.
type ProfileStore struct {
	db  bun.IDB
	enc crypt.Encryptor
}

var _ todo.ProfileStore = (*ProfileStore)(nil)

// NewProfileStore returns a ProfileStore over db, which may be a connection or
// a transaction.
func NewProfileStore(db bun.IDB, enc crypt.Encryptor) *ProfileStore {
	return &ProfileStore{db: db, enc: enc}
}

func nameBinding(userID uuid.UUID) []byte {
	return crypt.Bind("profiles", "name", userID.String())
}

func (s *ProfileStore) toModel(p todo.Profile) (*profileModel, error) {
	name, err := s.enc.Seal([]byte(p.Name), nameBinding(p.UserID))
	if err != nil {
		return nil, fmt.Errorf("storage: sealing a profile name: %w", err)
	}
	return &profileModel{UserID: bunstore.ID(p.UserID), Name: name}, nil
}

// fromModel opens the name with the caller's userID, not the row's, so a query
// that loses its user condition fails instead of returning another's name.
func (s *ProfileStore) fromModel(m *profileModel, userID uuid.UUID) (todo.Profile, error) {
	name, err := s.enc.Open(m.Name, nameBinding(userID))
	if err != nil {
		return todo.Profile{}, fmt.Errorf("storage: opening a profile name: %w", err)
	}
	return todo.Profile{UserID: userID, Name: string(name)}, nil
}

func (s *ProfileStore) CreateProfile(ctx context.Context, p todo.Profile) error {
	m, err := s.toModel(p)
	if err != nil {
		return err
	}
	if _, err := s.db.NewInsert().Model(m).Exec(ctx); err != nil {
		return fmt.Errorf("storage: creating a profile: %w", err)
	}
	return nil
}

func (s *ProfileStore) GetProfile(ctx context.Context, userID uuid.UUID) (todo.Profile, error) {
	m := new(profileModel)
	err := s.db.NewSelect().Model(m).Where("user_id = ?", bunstore.ID(userID)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return todo.Profile{}, todo.ErrNotFound
	}
	if err != nil {
		return todo.Profile{}, fmt.Errorf("storage: reading a profile: %w", err)
	}
	return s.fromModel(m, userID)
}

// LockProfile writes the row unchanged, which locks it on both dialects. Call it
// first in a transaction: on SQLite, a read before the first write can fail.
func (s *ProfileStore) LockProfile(ctx context.Context, userID uuid.UUID) error {
	res, err := s.db.NewRaw("UPDATE profiles SET user_id = user_id WHERE user_id = ?", bunstore.ID(userID)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("storage: locking a profile: %w", err)
	}
	return requireOneRow(res, "locking a profile")
}

// requireOneRow turns a statement that matched no row into todo.ErrNotFound.
func requireOneRow(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: %s: %w", what, err)
	}
	if n == 0 {
		return todo.ErrNotFound
	}
	return nil
}
