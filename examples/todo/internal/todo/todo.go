// Package todo holds the example application's domain types and the stores it
// needs. It does not import Bun directly, and no Bun type is in its contract.
package todo

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/borfast/sulis"
)

// Todo is one item on a person's list.
type Todo struct {
	ID, OwnerID          uuid.UUID
	Text                 string
	Done                 bool
	CreatedAt, UpdatedAt time.Time
}

// Profile is what the application keeps about a user beside Sulis's record.
type Profile struct {
	UserID uuid.UUID
	Name   string
}

// ErrNotFound means no row matched. A row owned by someone else does not match.
var ErrNotFound = errors.New("todo: not found")

// ProfileStore keeps profiles. GetProfile and LockProfile return ErrNotFound
// for a missing profile.
type ProfileStore interface {
	CreateProfile(ctx context.Context, p Profile) error
	GetProfile(ctx context.Context, userID uuid.UUID) (Profile, error)
	// LockProfile takes a write lock on the profile until the transaction ends.
	// It must be the transaction's first statement: Add and update rely on that.
	LockProfile(ctx context.Context, userID uuid.UUID) error
}

// TodoStore keeps to-dos. Get, Update and Delete match both the ID and the
// owner, and return ErrNotFound when nothing matches.
type TodoStore interface {
	CreateTodo(ctx context.Context, t Todo) error
	GetTodo(ctx context.Context, ownerID, id uuid.UUID) (Todo, error)
	// UpdateTodo writes Text, Done and UpdatedAt.
	UpdateTodo(ctx context.Context, t Todo) error
	DeleteTodo(ctx context.Context, ownerID, id uuid.UUID) error
	// ListTodos orders by creation time, then ID.
	ListTodos(ctx context.Context, ownerID uuid.UUID) ([]Todo, error)
	CountTodos(ctx context.Context, ownerID uuid.UUID) (int, error)
}

// Repos are the stores one transaction uses.
type Repos struct {
	Auth     *sulis.Sulis
	Profiles ProfileStore
	Todos    TodoStore
}
