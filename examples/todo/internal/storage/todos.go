package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/examples/todo/internal/todo"
)

type todoModel struct {
	bun.BaseModel `bun:"table:todos,alias:t"`

	ID        bunstore.ID  `bun:"id,pk,type:uuid"`
	OwnerID   bunstore.ID  `bun:"owner_id,type:uuid"`
	Text      crypt.Sealed `bun:"text"`
	Done      bool         `bun:"done"`
	CreatedAt time.Time    `bun:"created_at"`
	UpdatedAt time.Time    `bun:"updated_at"`
}

// TodoStore implements todo.TodoStore, sealing each text to its row and owner.
type TodoStore struct {
	db  bun.IDB
	enc crypt.Encryptor
}

var _ todo.TodoStore = (*TodoStore)(nil)

// NewTodoStore returns a TodoStore over db, which may be a connection or a
// transaction.
func NewTodoStore(db bun.IDB, enc crypt.Encryptor) *TodoStore {
	return &TodoStore{db: db, enc: enc}
}

func textBinding(id, ownerID uuid.UUID) []byte {
	return crypt.Bind("todos", "text", id.String(), ownerID.String())
}

func (s *TodoStore) toModel(t todo.Todo) (*todoModel, error) {
	text, err := s.enc.Seal([]byte(t.Text), textBinding(t.ID, t.OwnerID))
	if err != nil {
		return nil, fmt.Errorf("storage: sealing a to-do: %w", err)
	}
	return &todoModel{
		ID:        bunstore.ID(t.ID),
		OwnerID:   bunstore.ID(t.OwnerID),
		Text:      text,
		Done:      t.Done,
		CreatedAt: t.CreatedAt.UTC(),
		UpdatedAt: t.UpdatedAt.UTC(),
	}, nil
}

// fromModel opens the text with the IDs the caller asked for, not the row's, so
// a query that loses its owner condition fails instead of returning another's text.
func (s *TodoStore) fromModel(m *todoModel, id, ownerID uuid.UUID) (todo.Todo, error) {
	text, err := s.enc.Open(m.Text, textBinding(id, ownerID))
	if err != nil {
		return todo.Todo{}, fmt.Errorf("storage: opening a to-do: %w", err)
	}
	return todo.Todo{
		ID:        id,
		OwnerID:   ownerID,
		Text:      string(text),
		Done:      m.Done,
		CreatedAt: m.CreatedAt.UTC(),
		UpdatedAt: m.UpdatedAt.UTC(),
	}, nil
}

func (s *TodoStore) CreateTodo(ctx context.Context, t todo.Todo) error {
	m, err := s.toModel(t)
	if err != nil {
		return err
	}
	if _, err := s.db.NewInsert().Model(m).Exec(ctx); err != nil {
		return fmt.Errorf("storage: creating a to-do: %w", err)
	}
	return nil
}

func (s *TodoStore) GetTodo(ctx context.Context, ownerID, id uuid.UUID) (todo.Todo, error) {
	m := new(todoModel)
	err := s.db.NewSelect().Model(m).
		Where("id = ?", bunstore.ID(id)).
		Where("owner_id = ?", bunstore.ID(ownerID)).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return todo.Todo{}, todo.ErrNotFound
	}
	if err != nil {
		return todo.Todo{}, fmt.Errorf("storage: reading a to-do: %w", err)
	}
	return s.fromModel(m, id, ownerID)
}

func (s *TodoStore) UpdateTodo(ctx context.Context, t todo.Todo) error {
	m, err := s.toModel(t)
	if err != nil {
		return err
	}
	res, err := s.db.NewUpdate().Model(m).
		Column("text", "done", "updated_at").
		Where("id = ?", m.ID).
		Where("owner_id = ?", m.OwnerID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("storage: updating a to-do: %w", err)
	}
	return requireOneRow(res, "updating a to-do")
}

func (s *TodoStore) DeleteTodo(ctx context.Context, ownerID, id uuid.UUID) error {
	res, err := s.db.NewDelete().Model((*todoModel)(nil)).
		Where("id = ?", bunstore.ID(id)).
		Where("owner_id = ?", bunstore.ID(ownerID)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("storage: deleting a to-do: %w", err)
	}
	return requireOneRow(res, "deleting a to-do")
}

func (s *TodoStore) ListTodos(ctx context.Context, ownerID uuid.UUID) ([]todo.Todo, error) {
	var models []todoModel
	err := s.db.NewSelect().Model(&models).
		Where("owner_id = ?", bunstore.ID(ownerID)).
		Order("created_at", "id").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: listing to-dos: %w", err)
	}
	todos := make([]todo.Todo, 0, len(models))
	for i := range models {
		t, err := s.fromModel(&models[i], uuid.UUID(models[i].ID), ownerID)
		if err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	return todos, nil
}

func (s *TodoStore) CountTodos(ctx context.Context, ownerID uuid.UUID) (int, error) {
	n, err := s.db.NewSelect().Model((*todoModel)(nil)).
		Where("owner_id = ?", bunstore.ID(ownerID)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: counting to-dos: %w", err)
	}
	return n, nil
}
