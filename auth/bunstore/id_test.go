package bunstore_test

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestIDAppendQuery(t *testing.T) {
	u := uuid.NewV7()
	pg := schema.NewQueryGen(pgdialect.New())
	sqlite := schema.NewQueryGen(sqlitedialect.New())

	tests := []struct {
		name string
		gen  schema.QueryGen
		id   uuid.UUID
		want string
	}{
		{"postgres", pg, u, "'" + u.String() + "'"},
		{"postgres nil", pg, uuid.Nil(), "'00000000-0000-0000-0000-000000000000'"},
		{"sqlite", sqlite, u, "X'" + hex.EncodeToString(u[:]) + "'"},
		{"sqlite nil", sqlite, uuid.Nil(), "X'" + strings.Repeat("0", 32) + "'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bunstore.ID(tt.id).AppendQuery(tt.gen, nil)
			if err != nil {
				t.Fatalf("AppendQuery: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("AppendQuery = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIDAppendQueryRefusesOtherDialects(t *testing.T) {
	if _, err := bunstore.ID(uuid.NewV7()).AppendQuery(schema.NewNopQueryGen(), nil); err == nil {
		t.Fatal("AppendQuery with an unknown dialect returned nil, want an error")
	}
}

func TestIDScan(t *testing.T) {
	u := uuid.NewV7()

	accepted := []struct {
		name string
		src  any
		want uuid.UUID
	}{
		{"nil", nil, uuid.Nil()},
		{"16 bytes", u[:], u},
		{"text", u.String(), u},
		{"text bytes", []byte(u.String()), u},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			got := bunstore.ID(uuid.NewV7())
			if err := got.Scan(tt.src); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if uuid.UUID(got) != tt.want {
				t.Errorf("Scan = %v, want %v", got, tt.want)
			}
		})
	}

	refused := []struct {
		name string
		src  any
	}{
		{"15 bytes", make([]byte, 15)},
		{"17 bytes", make([]byte, 17)},
		{"malformed text", "not-a-uuid"},
		{"32 hex characters", strings.ReplaceAll(u.String(), "-", "")},
		{"integer", int64(1)},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			var got bunstore.ID
			if err := got.Scan(tt.src); err == nil {
				t.Errorf("Scan(%v) = nil, want an error", tt.src)
			}
		})
	}
}

type idRoundTrip struct {
	bun.BaseModel `bun:"table:id_round_trip"`

	ID  bunstore.ID  `bun:"id,pk"`
	Ref *bunstore.ID `bun:"ref"`
}

// createIDTable makes a table with one ID column and one nullable reference,
// in the column type each dialect uses for IDs.
func createIDTable(t *testing.T, db *bun.DB) {
	t.Helper()

	typ := "BLOB"
	if db.Dialect().Name() == dialect.PG {
		typ = "UUID"
	}
	q := "CREATE TABLE id_round_trip (id " + typ + " PRIMARY KEY, ref " + typ + ")"
	if _, err := db.ExecContext(t.Context(), q); err != nil {
		t.Fatalf("creating id_round_trip: %v", err)
	}
}

func TestIDRoundTrip(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		createIDTable(t, db)

		ref := bunstore.ID(uuid.NewV7())
		rows := []idRoundTrip{
			{ID: bunstore.ID(uuid.NewV7())},
			{ID: bunstore.ID(uuid.NewV7()), Ref: &ref},
		}
		for _, row := range rows {
			if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
				t.Fatalf("insert: %v", err)
			}

			var got idRoundTrip
			if err := db.NewSelect().Model(&got).Where("id = ?", row.ID).Scan(ctx); err != nil {
				t.Fatalf("select: %v", err)
			}
			if got.ID != row.ID {
				t.Errorf("ID = %v, want %v", got.ID, row.ID)
			}
			switch {
			case row.Ref == nil && got.Ref != nil:
				t.Errorf("Ref = %v, want nil", *got.Ref)
			case row.Ref != nil && (got.Ref == nil || *got.Ref != *row.Ref):
				t.Errorf("Ref = %v, want %v", got.Ref, *row.Ref)
			}
		}

		if db.Dialect().Name() == dialect.SQLite {
			var typ string
			var n int
			err := db.NewRaw("SELECT typeof(id), length(id) FROM id_round_trip WHERE id = ?", rows[0].ID).
				Scan(ctx, &typ, &n)
			if err != nil {
				t.Fatalf("reading the stored form: %v", err)
			}
			if typ != "blob" || n != 16 {
				t.Errorf("stored as %s of length %d, want blob of length 16", typ, n)
			}
		}
	})
}

func TestIDsSortByCreationTime(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		createIDTable(t, db)

		ids := make([]bunstore.ID, 5)
		for i := range ids {
			if i > 0 {
				time.Sleep(2 * time.Millisecond)
			}
			ids[i] = bunstore.ID(uuid.NewV7())
		}
		for i := len(ids) - 1; i >= 0; i-- {
			if _, err := db.NewInsert().Model(&idRoundTrip{ID: ids[i]}).Exec(ctx); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}

		var got []bunstore.ID
		if err := db.NewRaw("SELECT id FROM id_round_trip ORDER BY id").Scan(ctx, &got); err != nil {
			t.Fatalf("select: %v", err)
		}
		if len(got) != len(ids) {
			t.Fatalf("got %d IDs, want %d", len(got), len(ids))
		}
		for i := range ids {
			if got[i] != ids[i] {
				t.Errorf("position %d = %v, want %v", i, got[i], ids[i])
			}
		}
	})
}
