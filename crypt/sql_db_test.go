package crypt_test

import (
	"bytes"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/borfast/gorfast/crypt"
	"github.com/borfast/gorfast/internal/testdb"
)

type sealedRow struct {
	bun.BaseModel `bun:"table:crypt_sealed"`
	ID            string       `bun:"id,pk"`
	Body          crypt.Sealed `bun:"body"`
}

func TestSealedColumn(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		column := "BLOB"
		if db.Dialect().Name() == dialect.PG {
			column = "BYTEA"
		}
		ctx := t.Context()
		if _, err := db.NewRaw("CREATE TABLE crypt_sealed (id TEXT PRIMARY KEY, body " + column + ")").Exec(ctx); err != nil {
			t.Fatal(err)
		}

		kr := newKeyring(t, mustKey(t, crypt.XChaCha20Poly1305))
		binding := func(id string) []byte { return crypt.Bind("crypt_sealed", "body", id) }
		value := sealForKeyring(t, kr, []byte("secret"), binding("value"))
		empty := sealForKeyring(t, kr, []byte{}, binding("empty-plaintext"))
		for _, row := range []sealedRow{
			{ID: "value", Body: value},
			{ID: "empty-plaintext", Body: empty},
			{ID: "null"},
			{ID: "empty-bytes", Body: crypt.FromCiphertext([]byte{})},
		} {
			if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
				t.Fatalf("insert %s: %v", row.ID, err)
			}
		}

		var nullIDs []string
		if err := db.NewSelect().Model((*sealedRow)(nil)).Column("id").Where("body IS NULL").Scan(ctx, &nullIDs); err != nil {
			t.Fatal(err)
		}
		if len(nullIDs) != 1 || nullIDs[0] != "null" {
			t.Fatalf("SQL NULL rows = %v; want [null]", nullIDs)
		}

		var got sealedRow
		for _, id := range []string{"value", "null", "empty-plaintext", "empty-bytes"} {
			if err := db.NewSelect().Model(&got).Where("id = ?", id).Scan(ctx); err != nil {
				t.Fatalf("read %s: %v", id, err)
			}
			if got.ID != id {
				t.Fatalf("read %s: ID = %q", id, got.ID)
			}
			if id == "null" {
				if !got.Body.IsZero() {
					t.Fatal("SQL NULL did not reset the previously loaded ciphertext")
				}
				continue
			}
			if got.Body.IsZero() {
				t.Fatalf("%s scanned as the zero value", id)
			}
			switch id {
			case "value":
				if !bytes.Equal(got.Body.Ciphertext(), value.Ciphertext()) {
					t.Fatal("ciphertext changed during the database round trip")
				}
				requireKeyringPlaintext(t, kr, got.Body, []byte("secret"), binding(id))
			case "empty-plaintext":
				requireKeyringPlaintext(t, kr, got.Body, []byte{}, binding(id))
			case "empty-bytes":
				if len(got.Body.Ciphertext()) != 0 {
					t.Fatal("empty non-NULL bytes changed during the database round trip")
				}
				pt, err := kr.Open(got.Body, binding(id))
				requireCannotOpen(t, pt, err)
			}
		}
	})
}
