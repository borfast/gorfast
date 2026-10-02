# `crypt`: design

**Date:** 2026-10-02
**Status:** Approved in conversation, pending review of this written spec
**Replaces:** section 6.1 of `2026-09-27-gorfast-design.md`

## 1. Purpose

`crypt` makes encryption at rest easy to use correctly in a Gorfast
application: a column whose value must never be readable from the database
alone, such as a message body or a personal identifier.

It does not implement cryptographic primitives. It arranges audited ones,
AES-256-GCM from the Go standard library and XChaCha20-Poly1305 from
`golang.org/x/crypto`, into a small, documented format with key rotation and
binding of each value to where it is stored.

`crypt` is a primitive package under doctrine rule 1: any Gorfast package may
import it, and it imports nothing from Gorfast and nothing from Bun.

## 2. Decisions and the alternatives rejected

| Decision | Choice | Reason |
|---|---|---|
| Implementation | Our own envelope over standard primitives | The hard mathematics is in the standard library and `x/crypto` either way. What remains is a short, listable set of composition rules (section 5), each covered by a test. |
| Google Tink | Rejected | Its value is misuse-resistance across many primitives we do not use. Its keyset format is opaque in config, and without a KMS it loads keys through a package named `insecurecleartextkeyset`, the same trust model as a plain key with more complexity. If it ever becomes the right answer, it is one more implementation behind `Encryptor`, at the cost of re-encrypting stored data. |
| libsodium | Rejected | Reachable from Go only through cgo, which breaks pure-Go builds and cross-compilation. Its AEAD is XChaCha20-Poly1305, already available natively in `x/crypto`. |
| Algorithms | AES-256-GCM and XChaCha20-Poly1305, chosen per key | The user requires the choice. Each key is bound to exactly one algorithm. |
| Default for new keys | XChaCha20-Poly1305 | Its 24-byte random nonce has no practical limit per key. AES-256-GCM's 12-byte random nonce is safe for about 2^32 encryptions per key. For column-sized values the speed difference does not matter. |
| Binding to location | Required `context` argument on every `Seal` and `Open` | Without it, a ciphertext copied to another row still opens. Opting out means passing `nil`, a visible choice, per doctrine rule 5. |
| Column types | One `Sealed` type that only carries ciphertext | `driver.Valuer` and `sql.Scanner` receive no key, so a type that encrypted itself would need a global, which doctrine forbids. Sealing happens explicitly in a store's model conversion instead. |
| TOTP adapter from the original spec | Dropped | Sulis's `totp.Service` encrypts the secret itself before calling the store, and the store contract states the store never sees a usable secret. A Gorfast TOTP store stores an opaque string and needs nothing from `crypt`. |

## 3. Public API

```go
// Encryptor is what every caller depends on. It reveals nothing about the
// format or where keys come from.
type Encryptor interface {
    Seal(plaintext, context []byte) (Sealed, error)
    Open(s Sealed, context []byte) ([]byte, error)
}

// Sealed holds ciphertext only. Its bytes are unexported, so the only ways
// to obtain a non-empty Sealed are Seal and scanning one from a database.
type Sealed struct{ b []byte }

func (s Sealed) IsZero() bool

// Bind builds a context from parts without ambiguity: each part is
// preceded by its length as a 4-byte big-endian integer, so ("ab", "c")
// and ("a", "bc") differ.
func Bind(parts ...string) []byte

// Keyring is the Encryptor shipped with crypt: one current key that seals
// and any number of retired keys that only open.
type Keyring struct{ /* unexported */ }

func NewKeyring(current Key, retired ...Key) (*Keyring, error)
func (k *Keyring) Seal(plaintext, context []byte) (Sealed, error)
func (k *Keyring) Open(s Sealed, context []byte) ([]byte, error)
func (k *Keyring) NeedsReseal(s Sealed) bool
func (k *Keyring) Reseal(s Sealed, context []byte) (Sealed, bool, error)

// Key is one key bound to one algorithm. It never prints its material.
type Key struct{ /* unexported */ }

func ParseKey(spec string) (Key, error)       // "algorithm:base64"
func GenerateKey(alg Algorithm) (Key, error)
func (k Key) String() string                  // "algorithm:<key ID>"
func (k Key) Spec() string                    // "algorithm:base64"; the only way to export key material

type Algorithm uint8

const (
    AES256GCM         Algorithm = 0x01
    XChaCha20Poly1305 Algorithm = 0x02
)

// KeySource is where key material comes from. The first key is current.
type KeySource interface {
    Keys(ctx context.Context) ([]Key, error)
}

func StaticKeys(specs ...string) KeySource
func Load(ctx context.Context, src KeySource) (*Keyring, error)

var (
    ErrUnknownKey = errors.New("crypt: sealed under a key not in the keyring")
    ErrCannotOpen = errors.New("crypt: cannot open sealed value")
)
```

`Reseal` opens a value and seals it again under the current key, and reports
whether it changed anything. A value already under the current key is
returned unchanged.

Usage in a store:

```go
type messageModel struct {
    ID   string       `bun:"id,pk"`
    Body crypt.Sealed `bun:"body"`
}

body, err := s.enc.Seal([]byte(m.Body), crypt.Bind("messages", "body", m.ID))
```

A consequence: a store's model conversions return an error when they seal or
open, unlike the pure conversions in `auth/bunstore`. Sealing can genuinely
fail.

Wiring stays explicit in `main`, with no global and nothing at init time:

```go
enc, err := crypt.Load(ctx, crypt.StaticKeys(strings.Split(cfg.Crypt.Keys, ",")...))
```

## 4. Format

Every sealed value, version 1:

```
offset  size       field
0       1          version     0x01
1       1          algorithm   0x01 = AES-256-GCM, 0x02 = XChaCha20-Poly1305
2       8          key ID
10      12 or 24   nonce       size fixed by the algorithm
...     rest       ciphertext followed by the 16-byte tag
```

Overhead is 38 bytes with AES-256-GCM and 50 with XChaCha20-Poly1305.

- **Authenticated data** is the 10-byte header followed by the context. The
  header has a fixed length, so the concatenation is unambiguous. Changing any
  header byte, any ciphertext byte, or the context makes `Open` fail.
- **Key ID** is HMAC-SHA256 keyed with the key over the fixed label
  `gorfast/crypt key id v1`, truncated to 8 bytes. It identifies a key without
  revealing anything usable about it.
- **Version** lets the format change later. A new version adds a new layout;
  version 1 values keep opening for as long as the package exists.

### Storage

`Sealed` implements `driver.Valuer` and `sql.Scanner` and moves bytes only:
`bytea` on Postgres, `BLOB` on SQLite. The zero `Sealed` writes SQL `NULL`
and scans back from `NULL`; `IsZero` reports it. Sealing an empty plaintext
produces a real, non-zero value. Nullable encrypted columns can therefore tell
"no value" from "empty value". `Scan` accepts `[]byte` and `string` sources,
treating a string as the same raw bytes rather than as any text encoding,
because some drivers return binary columns as strings.

## 5. Composition rules

These are the parts of correct encryption that `crypt` owns. Each has tests in
section 8.

1. **Nonces** come from `crypto/rand`, at exactly the size the algorithm
   requires. If the random source fails, `Seal` returns an error; there is no
   fallback.
2. **The header is attacker-controlled** because it lives in the database. It
   is part of the authenticated data, and each key is bound to one algorithm:
   a header naming an algorithm other than its key's is rejected before any
   decryption is attempted.
3. **Key separation.** One key, one algorithm. `Load` and `NewKeyring` refuse
   two keys with the same ID, which also refuses the same key material listed
   twice.
4. **Fail closed.** `Open` returns `ErrUnknownKey` when the header names a key
   the keyring does not hold, and `ErrCannotOpen` for every other failure:
   tampering, wrong context, truncation, unknown version or algorithm, or an
   algorithm mismatch. `ErrUnknownKey` reveals nothing, since the key ID is
   readable in the header, and it identifies the most common operational
   mistake: removing a retired key too early.
5. **Keys are random, never derived from passwords.** A key is exactly 32
   random bytes. There is no per-value key derivation.

## 6. Keys

### Configuration

A key is written `algorithm:base64`, where the base64 decodes to exactly 32
bytes. Algorithm names are `xchacha20poly1305` and `aes256gcm`. All keys go in
one variable, comma-separated, current first:

```
MYAPP_CRYPT__KEYS="xchacha20poly1305:q3Jv...=,aes256gcm:Lm9k...="
```

Neither `:` nor `,` occurs in standard base64, so parsing is unambiguous.
Following `skills/loading-configuration`, the application's config struct
holds the string and `main` passes the split specs to `StaticKeys`.

`Load` refuses, at startup: no keys, a key that does not decode to exactly 32
bytes, an unknown algorithm name, and two keys with the same ID.

### Generating a key

Nothing to install, and no Tink or KMS:

```
go run github.com/borfast/gorfast/crypt/cmd/newkey@latest              # XChaCha20-Poly1305
go run github.com/borfast/gorfast/crypt/cmd/newkey@latest -alg aes256gcm
printf 'xchacha20poly1305:%s\n' "$(openssl rand -base64 32)"            # by hand
```

When `cmd/gorfast` exists, `newkey` becomes a subcommand.

### Rotation

Two phases, so that instances running different configurations never seal
with a key the others cannot open:

1. Add the new key **second** in the list, which makes it open-only. Deploy
   to every instance.
2. Move it **first**, which makes it current. Deploy again. New values use it;
   existing values still open under their old key.
3. Reseal existing values. The application iterates its own rows and calls
   `Reseal`; `NeedsReseal` lets it count what remains. Gorfast cannot write
   this loop because it does not know the application's tables. A background
   version fits naturally once `jobs` exists.
4. Once no value uses the old key, remove it. If one still did, opening it
   returns `ErrUnknownKey`.

### Printing

`Key` implements `String`, `GoString` and `Format` to print
`algorithm:<key ID>`, so `%v`, `%+v`, `%#v` and `%s` never show key material.
`crypt` does not claim to zero key memory; Go offers no reliable way to
guarantee it.

`Spec` is the single, explicitly named method that returns a key in its
configuration form, material included. `newkey` uses it to print a generated
key; nothing else in Gorfast calls it.

## 7. Package layout

```
crypt/
  crypt.go       Encryptor, Sealed, Bind, ErrUnknownKey, ErrCannotOpen
  key.go         Key, Algorithm, ParseKey, GenerateKey, key ID, printing
  keyring.go     Keyring: Seal, Open, NeedsReseal, Reseal
  aead.go        the two algorithms behind one internal table
  source.go      KeySource, StaticKeys, Load
  sql.go         Sealed as driver.Valuer and sql.Scanner
  cmd/newkey/    the key generator
  testdata/vectors.json
```

`golang.org/x/crypto` becomes a direct dependency. It is already in the module
graph indirectly.

## 8. Testing

- **Frozen format.** `testdata/vectors.json` holds sealed values produced at
  implementation time for both algorithms, with their keys, plaintexts and
  contexts. A test asserts they keep opening. A change that could no longer
  read version 1 fails CI. Tests produce stable vectors by injecting a fixed
  nonce source through an unexported hook; production code always uses
  `crypto/rand`.
- **Round trips** for both algorithms with empty, small and large plaintexts.
- **Tampering.** Every byte of a real sealed value is flipped in turn and each
  result must fail to open. Also every truncation length, the wrong context,
  swapped `Bind` parts, and a header that names the other algorithm for its
  key.
- **Rotation end to end.** Seal under key A; rotate to B; A's value still
  opens; `NeedsReseal` is true; `Reseal`; it is false; remove A and an
  unresealed value returns `ErrUnknownKey`.
- **Startup refusals.** Every case `Load` refuses, from section 6.
- **No key leaks.** `%v`, `%+v`, `%#v` and `%s` on a `Key` never contain the
  key material.
- **Fuzzing.** `Open` on arbitrary input never panics and never succeeds.
- **A real database.** A `Sealed` column round-trips through real Postgres and
  SQLite, including `NULL` for the zero value, per doctrine rule 8. The test
  uses `internal/testdb`, which keeps Bun out of the package's non-test
  imports.

## 9. Out of scope

- **HMAC blind indexes** for looking up encrypted values. HMAC is in the
  standard library, so no new dependency is needed when the time comes.
- **A KMS key source**, which would unwrap KMS-wrapped data keys once at
  startup. Planned as its own piece of work; it implements `KeySource` and
  does not change the format.
- **Secrets-manager support** for configuration, such as Vault or AWS Secrets
  Manager. It applies to every secret, not only keys, so it belongs in
  `core/config`. Planned separately.
- **Streaming encryption** for large files.
- **Migrating `lastmessage`** onto `crypt`. Worth doing, separately; the note
  in that project's `AGENTS.md` already points in this direction.
