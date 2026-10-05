# `crypt`: design

**Date:** 2026-10-02
**Status:** Approved in conversation; revised after
`docs/reviews/2026-10-02-architecture-design-review.md`; pending review of this
written spec
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
| Default for new keys | XChaCha20-Poly1305 | Its 24-byte random nonce has no practical limit per key. AES-256-GCM with random nonces has a hard limit per key, which section 6 turns into an operational requirement. For column-sized values the speed difference does not matter. |
| Binding to location | Required `binding` argument on every `Seal` and `Open` | Without it, a ciphertext copied to another row still opens. Opting out means passing `nil`, a visible choice, per doctrine rule 5. Section 6 states what a binding must contain. |
| Column types | One `Sealed` type that only carries ciphertext | `driver.Valuer` and `sql.Scanner` receive no key, so a type that encrypted itself would need a global, which doctrine forbids. Sealing happens explicitly in a store's model conversion instead. |
| TOTP adapter from the original spec | Dropped | Sulis's `totp.Service` encrypts the secret itself before calling the store, and the store contract states the store never sees a usable secret. A Gorfast TOTP store stores an opaque string and needs nothing from `crypt`. |

## 3. Public API

```go
// Encryptor is what every caller depends on. It reveals nothing about the
// format or where keys come from. Other implementations must pass crypttest.
type Encryptor interface {
    Seal(plaintext, binding []byte) (Sealed, error)
    Open(s Sealed, binding []byte) ([]byte, error)
}

// Sealed carries ciphertext. It is a struct, not a []byte, so plaintext
// cannot become a Sealed by accidental type conversion.
type Sealed struct{ b []byte }

func (s Sealed) IsZero() bool
func (s Sealed) Ciphertext() []byte          // a copy of the bytes

// FromCiphertext copies bytes produced by an Encryptor. It exists for other
// Encryptor implementations and for moving values between systems.
func FromCiphertext(b []byte) Sealed

// Bind builds a binding from parts without ambiguity: each part is
// preceded by its length as a 4-byte big-endian integer, so ("ab", "c")
// and ("a", "bc") differ.
func Bind(parts ...string) []byte

// Keyring is the Encryptor shipped with crypt: one current key that seals
// and any number of retired keys that only open.
type Keyring struct{ /* unexported */ }

func NewKeyring(current Key, retired ...Key) (*Keyring, error)
func (k *Keyring) Seal(plaintext, binding []byte) (Sealed, error)
func (k *Keyring) Open(s Sealed, binding []byte) ([]byte, error)
func (k *Keyring) NeedsReseal(s Sealed) bool
func (k *Keyring) Reseal(s Sealed, binding []byte) (Sealed, bool, error)

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
// Other implementations must pass crypttest.
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

### What `Sealed` does and does not protect

Being a struct stops plaintext from becoming a `Sealed` by accident, through a
type conversion such as `Sealed(b)`. It does not stop deliberate misuse:
`FromCiphertext` and `Scan` both accept any bytes, because other `Encryptor`
implementations and the database must be able to supply them. Wrapping bytes
does not authenticate them; `Open` establishes validity against a key and
binding. Invalid ciphertext fails to open.

### Byte ownership

`Sealed` owns its bytes. `FromCiphertext` and `Scan` copy incoming byte slices;
`Ciphertext` and a non-NULL `Value` return copies. None of these methods
exposes the internal slice. Mutating an input buffer or a returned buffer
cannot change an existing `Sealed`, including one returned unchanged by
`Reseal`. `Scan` must copy driver-owned bytes before returning, because the
driver may reuse them on its next call (the
[`sql.Scanner` contract](https://pkg.go.dev/database/sql#Scanner)).

### `Reseal` and `NeedsReseal`

- **`Reseal` is authoritative.** It always opens the value first, which
  authenticates it against the binding, even when the header names the current
  key. A value that fails to open returns the error from `Open`. A value that
  opens under the current key is returned unchanged with `false`; a value that
  opens under a retired key is sealed again under the current key and returned
  with `true`.
- **`NeedsReseal` is a hint.** It reads the header only and authenticates
  nothing. It returns `true` only when the header is well formed and names a
  retired key in this keyring. It returns `false` for the zero value, for a
  malformed header, for a key the keyring does not hold, and for the current
  key. It is for reporting rotation progress, never for deciding that a value
  is valid or that migration is complete. A `false` result does not replace
  `Open` or `Reseal`; section 6 defines the completion check.

### Usage

In a store:

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

- **Authenticated data** is the 10-byte header followed by the binding. The
  header has a fixed length, so the concatenation is unambiguous. Changing any
  header byte, any ciphertext byte, or the binding makes `Open` fail. A `nil`
  binding and an empty binding are the same binding.
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
   tampering, wrong binding, truncation, unknown version or algorithm, an
   algorithm mismatch, or the zero value. `ErrUnknownKey` reveals nothing,
   since the key ID is readable in the header, and it identifies the most
   common operational mistake: removing a retired key too early.
5. **Keys are random, never derived from passwords.** A key is exactly 32
   random bytes. There is no per-value key derivation.

## 6. Keys, bindings and rotation

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

### The AES-256-GCM limit is an operational requirement

With random 96-bit nonces, Go documents a maximum of 2^32 encryptions per
AES-GCM key ([`cipher.NewGCMWithRandomNonce`](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)).
The budget belongs to the key, so it is shared by every instance and process
that seals with it, across the key's whole lifetime. An application that
chooses AES-256-GCM must rotate the key well before its total number of
`Seal` calls approaches that limit. `crypt` does not count seals, because it
cannot see other instances. This is the main reason XChaCha20-Poly1305 is the
default: its 192-bit nonces make the limit irrelevant in practice.

### What a binding must contain

A binding names the slot a value belongs to. The application chooses it, so
these rules belong to the application:

- **Use stable identifiers.** Bind to a primary key, never to a value that can
  change, such as an email address.
- **Include the tenant** where row IDs are only unique within a tenant.
- **Changing a binding is a migration.** Renaming a table or column component
  of a binding means opening each value with the old binding and sealing it
  with the new one.
- **Binding does not prevent replay at the same location.** Writing an older
  ciphertext for the same slot back into it still opens. Detecting this needs
  trusted freshness state, such as an expected version used in the binding
  that cannot be rolled back alongside the ciphertext. A version counter in
  the same database row does not protect against an attacker who can restore
  both fields together. `crypt` does not provide trusted freshness state.

### Rotation

Two phases, so that instances running different configurations never seal
with a key the others cannot open:

1. Add the new key **second** in the list, which makes it open-only. Deploy
   to every instance.
2. Move it **first**, which makes it current. Deploy again. New values use it;
   existing values still open under their old key.
3. Reseal existing values, as described below.
4. Remove the old key from the configuration, and archive it, as described
   below.

### Resealing existing values

Gorfast cannot write the resealing loop, because it does not know the
application's tables. The application's loop must follow these rules:

- **Start only after phase 2 has reached every instance.** Until then, some
  instance may still seal new values with the old key.
- **Authenticate every non-NULL value.** Call `Reseal` with the row's binding,
  even when `NeedsReseal` returns `false`. A successful unchanged result proves
  the value opens under the current key. Skip SQL `NULL` only where the column
  is intentionally nullable. Record unknown keys, malformed values, wrong
  bindings and other failures as unresolved errors; do not silently skip them
  or replace them with `NULL`.
- **Write conditionally.** Read the row, `Reseal` the value, then update only
  if the column still holds the ciphertext that was read (or the row's version
  is unchanged). Zero rows affected means the row changed or was deleted;
  re-read it. If it still exists, authenticate the new value and retry or
  requeue it if it still needs resealing. An unrelated column edit may have
  changed the row version while leaving the encrypted value under the old
  key. Do not count a conflict as a completed migration. An unconditional
  write can overwrite an edit made between the read and the write.
- **Finish with validation and counting.** After phase 2, completion requires
  a full validation pass over the migration's columns, zero remaining values
  under retired keys, and zero unresolved validation errors, write failures or
  pending retries. `NeedsReseal` can report a progress count, but zero alone is
  insufficient: unknown keys and malformed headers also return `false`.
  Validate non-NULL values through `Open` or `Reseal` with their bindings, and
  retain the retired keys until these completion conditions hold.

A ready-made background version fits naturally once `jobs` exists.

### Retiring a key

Removing a key from the configuration does not make data encrypted under it
disposable. **Backups taken before resealing still contain values sealed with
the old key**, and restoring one needs that key. Archive every retired key in
secure storage outside the application configuration, for at least as long as
any backup that may contain its values is kept. A restored backup that
returns `ErrUnknownKey` needs a key from that archive added back as a retired
key.

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
  crypt.go       Encryptor, Sealed, FromCiphertext, Bind, ErrUnknownKey, ErrCannotOpen
  key.go         Key, Algorithm, ParseKey, GenerateKey, key ID, printing
  keyring.go     Keyring: Seal, Open, NeedsReseal, Reseal
  aead.go        the two algorithms behind one internal table
  source.go      KeySource, StaticKeys, Load
  sql.go         Sealed as driver.Valuer and sql.Scanner
  crypttest/     conformance suites for Encryptor and KeySource
  cmd/newkey/    the key generator
  testdata/vectors.json
```

`golang.org/x/crypto` becomes a direct dependency. It is already in the module
graph indirectly.

## 8. Testing

Two layers, per doctrine rule 3: portable conformance suites that any
implementation of the interfaces must pass, and tests specific to `Keyring`
and its format.

### Conformance suites (`crypt/crypttest`)

Public API, the way Sulis ships `storetest`:

- **`RunEncryptor(t, factory func() crypt.Encryptor)`**: a value round-trips;
  sealing the same plaintext twice gives different ciphertexts; an empty
  plaintext round-trips and seals to a non-zero value; opening with a
  different binding fails; flipping any byte of a sealed value makes it fail;
  every truncation fails; the zero value and specified invalid byte sequences
  supplied through `FromCiphertext` fail to open; each of these opening
  failures satisfies `errors.Is` with `ErrCannotOpen` or `ErrUnknownKey`.
  A valid value exported with `Ciphertext()` and reconstructed with
  `FromCiphertext` still opens to the same plaintext with the same binding.
- **`RunKeySource(t, factory func() crypt.KeySource)`**: `Keys` returns at
  least one key or an error; repeated calls return the same key IDs in the same
  order; an already-cancelled context makes it return an error rather than
  block.

`Keyring` and `StaticKeys` run these suites. A later KMS key source runs
`RunKeySource`.

### `Sealed` ownership tests

- Mutating the source slice after `FromCiphertext` or `Scan` cannot change
  the stored ciphertext. Reusing a simulated driver buffer after `Scan`
  cannot corrupt a previously scanned value.
- Mutating bytes returned by `Ciphertext()` or `Value()` cannot change the
  original `Sealed` or a copy returned unchanged by `Reseal`.
- These checks also verify that a valid value still opens after the external
  buffers have been mutated.

### `Keyring`-specific tests

- **Frozen format.** `testdata/vectors.json` holds sealed values produced at
  implementation time for both algorithms, with their keys, plaintexts and
  bindings. A test asserts they keep opening. A change that could no longer
  read version 1 fails CI. Tests produce stable vectors by injecting a fixed
  nonce source through an unexported hook; production code always uses
  `crypto/rand`.
- **Header handling.** A header naming the other algorithm for its key, an
  unknown version, and an unknown algorithm all return `ErrCannotOpen`; an
  unknown key ID returns `ErrUnknownKey`.
- **Rotation end to end.** Seal under key A; rotate to B; A's value still
  opens; `NeedsReseal` is true; `Reseal` returns a new value and `true`; for
  that value `NeedsReseal` is false and `Reseal` returns it unchanged with
  `false`; remove A and an unresealed value returns `ErrUnknownKey`.
- **`Reseal` authenticates.** A tampered value under the current key, and a
  valid value under the current key opened with the wrong binding, both
  return an error from `Reseal`, never the value unchanged.
- **`NeedsReseal` cases.** Zero value, malformed header, unknown key and
  current key all return `false`. A value with an intact retired-key header
  and a damaged authentication tag still returns `true`, while `Reseal`
  rejects it. A damaged value naming the current key returns `false`, while
  `Reseal` rejects it. These cases prove that the hint is not validation.
- **Startup refusals.** Every case `Load` refuses, from section 6.
- **No key leaks.** `%v`, `%+v`, `%#v` and `%s` on a `Key` never contain the
  key material.
- **Fuzzing.** `Open` and `NeedsReseal` never panic on any input. The fuzz
  test asserts only that; whether valid values open and specified mutations
  are rejected is covered by the deterministic tests above, because a fuzzer
  seeded with valid ciphertexts would correctly see some of them open.
- **A real database.** A `Sealed` column round-trips through real Postgres and
  SQLite, including `NULL` for the zero value, per doctrine rule 8. The test
  uses `internal/testdb`, which keeps Bun out of the package's non-test
  imports.

### Application resealing integration tests

When the example application adds its resealing loop, its plan must include
real Postgres and SQLite tests for section 6's persistence protocol:

- A concurrent edit of the encrypted field is preserved when the conditional
  migration write loses the race; the replacement value is authenticated.
- A concurrent edit of another field changes the row version but leaves the
  old ciphertext in place; the loop retries or requeues and eventually
  reseals it instead of silently skipping it.
- An unknown-key value and a malformed value each prevent completion even
  when the `NeedsReseal` count is zero. Pending retries and failed writes also
  prevent completion.
- A valid current-key value is authenticated without being rewritten, and an
  intentional SQL `NULL` is skipped without treating invalid ciphertext as
  an absent value.

These belong to the application's plan, not the standalone `crypt` package's
acceptance criteria: `crypt` does not own tables or run migrations.

## 9. Out of scope

- **HMAC blind indexes** for looking up encrypted values. HMAC is in the
  standard library, so no new dependency is needed when the time comes.
- **A KMS key source**, which would unwrap KMS-wrapped data keys once at
  startup. Planned as its own piece of work; it implements `KeySource`, passes
  `RunKeySource`, and does not change the format.
- **Secrets-manager support** for configuration, such as Vault or AWS Secrets
  Manager. It applies to every secret, not only keys, so it belongs in
  `core/config`. Planned separately.
- **Detecting replay** of an older ciphertext at the same location; see
  section 6.
- **Counting AES-256-GCM seals** across instances; see section 6.
- **Streaming encryption** for large files.
- **Migrating `lastmessage`** onto `crypt`. Worth doing, separately; the note
  in that project's `AGENTS.md` already points in this direction.
