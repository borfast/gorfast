package bunstore

import (
	"database/sql"
	"encoding"
	"fmt"
	"uuid"

	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/schema"
)

// ID is a sulis ID as a Bun value: a native uuid on Postgres, 16 bytes on SQLite.
// A model field needs `type:uuid` in its bun tag for Bun's CreateTable and Bulk.
type ID uuid.UUID

var (
	_ schema.QueryAppender     = ID{}
	_ sql.Scanner              = (*ID)(nil)
	_ encoding.TextMarshaler   = ID{}
	_ encoding.TextUnmarshaler = (*ID)(nil)
)

// AppendQuery writes id as UUID text on Postgres and as 16 raw bytes on SQLite.
func (id ID) AppendQuery(gen schema.QueryGen, b []byte) ([]byte, error) {
	switch name := gen.Dialect().Name(); name {
	case dialect.PG:
		return gen.Dialect().AppendString(b, uuid.UUID(id).String()), nil
	case dialect.SQLite:
		return gen.Dialect().AppendBytes(b, id[:]), nil
	default:
		return nil, fmt.Errorf("bunstore: no ID form for dialect %s", name)
	}
}

// Scan reads an ID as 16 raw bytes or as UUID text. NULL gives uuid.Nil().
func (id *ID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*id = ID(uuid.Nil())
		return nil
	case []byte:
		if len(v) == len(id) {
			copy(id[:], v)
			return nil
		}
		return id.parse(string(v))
	case string:
		return id.parse(v)
	default:
		return fmt.Errorf("bunstore: reading an ID: unexpected type %T", src)
	}
}

// parse accepts only the 36-character form, which every driver returns.
func (id *ID) parse(text string) error {
	if len(text) != 36 {
		return fmt.Errorf("bunstore: reading an ID: %d characters, want 36", len(text))
	}
	u, err := uuid.Parse(text)
	if err != nil {
		return fmt.Errorf("bunstore: reading an ID: %w", err)
	}
	*id = ID(u)
	return nil
}

// nullableID maps uuid.Nil() to nil, which a nullable column stores as NULL.
func nullableID(u uuid.UUID) *ID {
	if u == uuid.Nil() {
		return nil
	}
	id := ID(u)
	return &id
}

// fromNullableID maps a NULL column, read as nil, back to uuid.Nil().
func fromNullableID(id *ID) uuid.UUID {
	if id == nil {
		return uuid.Nil()
	}
	return uuid.UUID(*id)
}

// String returns the UUID text form.
func (id ID) String() string { return uuid.UUID(id).String() }

// MarshalText writes the UUID text form, so JSON shows an ID as uuid.UUID does.
func (id ID) MarshalText() ([]byte, error) { return uuid.UUID(id).MarshalText() }

// UnmarshalText accepts every form uuid.UUID accepts, unlike Scan.
func (id *ID) UnmarshalText(text []byte) error { return (*uuid.UUID)(id).UnmarshalText(text) }
