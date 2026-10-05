# Security Review Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the five open items from the 2026-10-03 security review of `crypt` and `auth/bunstore`: four documentation gaps and one behaviour gap in `UserStore.DeleteUser`.

**Architecture:** Four items are text added to the crypt spec, the design spec and the README, with no code change. The fifth makes `bunstore.UserStore.DeleteUser` remove the user's sessions and tokens in the same transaction as the user row, because the schema has no foreign keys and Sulis itself never calls `DeleteUser`, so an application that calls it directly would otherwise leave live sessions behind (doctrine rule 5).

**Tech Stack:** Go 1.27, Bun, Postgres 18 and SQLite through `internal/testdb`. No new dependencies.

**Spec:** The review findings in this conversation, recorded in each task below. The documents being edited are `docs/superpowers/specs/2026-10-02-crypt-design.md` ("the crypt spec") and `docs/superpowers/specs/2026-09-27-gorfast-design.md` ("the design spec").

## Global Constraints

- Branch `review-follow-ups` from `main` at `f9e66db`. `main` requires the `test` check, signed commits and linear history: merge through a pull request with a squash, or the user fast-forward pushes the branch.
- The working tree may still hold the user's uncommitted edits to `CLAUDE.md` and both specs. Before Task 1 the user commits or stashes them. Stage only the files a task names.
- No em-dashes anywhere. Code comments at most 2 lines. Commit subjects in the repo style ("Add ...", sentence case, no prefix); bodies at most 5 lines.
- Per `CLAUDE.md`: run `node .gitnexus/run.cjs impact "DeleteUser" --direction upstream --repo .` before editing `DeleteUser`, and `node .gitnexus/run.cjs detect-changes --scope all --repo .` before each commit. The second must list only the task's files.
- Postgres tests need `docker compose up -d postgres` and `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`. A skipped Postgres subtest is not a pass (doctrine rule 8).
- Task 1 step 3 downloads a module from the Go module proxy. Tell the user before running it (`AGENTS.md`, "Installing things").

## Decisions this plan adds

- **`DeleteUser` cascades in the store, not through foreign keys.** A foreign key from `sessions.user_id` to `users.id` is rejected because `storetest` creates sessions for user IDs that never exist, and `tokens.user_id` is legitimately empty for magic links.
- **`DeleteUser` of a missing user still returns `nil`**, as today. `DeleteUser("")` deletes nothing and returns `nil`: tokens with an empty `user_id` belong to no user and must survive.
- **The pinned `newkey` revision is the commit that merged `crypt`**, `f9e66db3e375288edb3fa9fb23dd6d55740f1456`. The repository has no Go-style `vX.Y.Z` tags, so a commit is the only pin available today.

## Review Focus

1. **`DeleteUser("")`** must not delete magic-link tokens, whose `user_id` is `''`. Test: Task 2 `TestDeleteUserMissingAndEmptyID`.
2. **A caller's transaction.** The cascade runs as a savepoint inside `bun.Tx`; its deletes are visible through that transaction and disappear if the caller rolls back. Test: Task 2 `TestDeleteUserInsideCallerTransaction`.
3. **Other users' rows** are untouched by the cascade. Test: Task 2 `TestDeleteUserRemovesSessionsAndTokens`.
4. **The Sulis conformance suite** still passes on both dialects after the change. Test: Task 2 step 5 runs the existing `TestUserStore`.
5. **The pinned `go run` form** resolves through the module proxy and prints a key `ParseKey` accepts. Test: Task 1 step 3.

---

### Task 1: Document the key handling risks and pin `newkey`

**Files:**
- Modify: `docs/superpowers/specs/2026-10-02-crypt-design.md` (section 6 "Configuration", section 6 "Generating a key", a new section 6 subsection, section 9)
- Modify: `docs/superpowers/specs/2026-09-27-gorfast-design.md` (section 9 "Known gaps")
- Modify: `README.md` ("Go module" section)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing code-facing. The crypt spec's section 6 heading `## 6. Keys, bindings and rotation` must keep its exact text, because the README links to its anchor.

- [ ] **Step 1: Edit the crypt spec**

Add these, with this wording (adjust only to fit surrounding sentences):

a. Section 6 "Configuration", a new paragraph after the sentence that begins "`Load` refuses, at startup":

> **An environment variable is not a secure store.** Every process running as the same user can read it from `/proc/<pid>/environ`, and so can a crash dump, `docker inspect`, and any child process the application starts. It holds every key, retired ones included, so one leak exposes backups as well as live data. The KMS key source planned in section 9 takes the material out of the configuration. Until then, treat this variable as the most sensitive value the application has, and keep it out of logs, shell history and CI output.

b. Section 6, a new subsection `### Ciphertext length reveals plaintext length`, placed after "The AES-256-GCM limit is an operational requirement":

> A sealed value is exactly 38 or 50 bytes longer than its plaintext, so anyone who can read the column learns the plaintext's length. For free text that is usually harmless. For a short field with few possible values, such as a yes or no answer or a country code, the length is the value. Pad such plaintexts to a fixed length before sealing and strip the padding after opening. `crypt` does not pad, because only the application knows which fields need it.

c. Section 6 "Generating a key": replace both `@latest` commands with `@f9e66db3e375288edb3fa9fb23dd6d55740f1456`, keep the `openssl` line, and add after the code block:

> Pin the revision. `@latest` runs whatever the module proxy serves at that moment, and a key generator is exactly the code that must be the code you reviewed. The commit above merged `crypt`; replace it with a later reviewed commit, or a release tag once the module has one. The `openssl` form avoids the question entirely.

d. Section 9, the "HMAC blind indexes" bullet, append:

> When built, the index key must be a separate key, never an AEAD key: one key, one purpose. Rotating an index key means recomputing every index value, which is a different migration from resealing.

e. Section 9, a new bullet after "Streaming encryption":

> **Padding** plaintexts to hide their length; see section 6.

- [ ] **Step 2: Edit the design spec and the README**

Design spec section 9: replace the bullet that begins "**Gorfast has no CI.**" with:

> **CI exists since 2026-10-04** (`.github/workflows/ci.yml`): vet, race tests on SQLite and Postgres 18, a 30 second fuzz run and `govulncheck`, on every pull request and push to `main`. A skipped Postgres subtest fails the job, and `main` requires the `test` check.

README "Go module": in the `newkey` sentence replace `@latest` with `@f9e66db3e375288edb3fa9fb23dd6d55740f1456` and append ", pinned to a reviewed commit". Keep the existing link to the crypt spec.

- [ ] **Step 3: Verify**

Run, after telling the user it downloads `github.com/borfast/gorfast` at that commit from the module proxy:

```bash
cd "$(mktemp -d)" && go run github.com/borfast/gorfast/crypt/cmd/newkey@f9e66db3e375288edb3fa9fb23dd6d55740f1456
```
Expected: one line starting `xchacha20poly1305:`.

```bash
cd /home/borfast/projects/gorfast
git diff -U0 -- docs README.md | grep '^+' | grep -c $'\xe2\x80\x94'
grep -c '^## 6. Keys, bindings and rotation$' docs/superpowers/specs/2026-10-02-crypt-design.md
grep -c '@latest' README.md docs/superpowers/specs/2026-10-02-crypt-design.md
```
Expected: `0`, then `1`, then `0` for both files.

- [ ] **Step 4: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add README.md docs/superpowers/specs/2026-10-02-crypt-design.md docs/superpowers/specs/2026-09-27-gorfast-design.md
git commit -m "Document crypt key handling risks and pin newkey"
```

---

### Task 2: `DeleteUser` removes the user's sessions and tokens

**Files:**
- Modify: `auth/bunstore/user.go:177-184`
- Test: `auth/bunstore/user_delete_test.go` (package `bunstore_test`)

**Interfaces:**
- Consumes: `testdb.Each`, `bunstore.NewUserStore`, `bunstore.NewSessionStore`, `bunstore.NewTokenStore`; `sulis.User`, `sulis.Session`, `sulis.Token`, `sulis.ErrUserNotFound`, `sulis.ErrTokenNotFound`.
- Produces: `func (s *UserStore) DeleteUser(ctx context.Context, id string) error`, signature unchanged. Inside one `s.db.RunInTx(ctx, nil, ...)` it deletes from `sessions` where `user_id = id`, from `tokens` where `user_id = id`, then from `users` where `id = id`. `id == ""` returns `nil` before any statement. A missing user returns `nil`. Errors are wrapped `bunstore: deleting user: %w`. Doc comment, 2 lines: the user and every session and token that name the user are removed together, so a deleted account cannot keep signing in.

- [ ] **Step 1: Impact analysis**

Run: `node .gitnexus/run.cjs impact "DeleteUser" --direction upstream --repo .`
Expected: callers are tests only, or `UNKNOWN`. Either way confirm with `grep -rn 'DeleteUser(' --include='*.go' .` that no non-test caller exists before editing.

- [ ] **Step 2: Write the failing tests**

Fixtures: a helper `seed(t, db)` creates users `alice` and `bob` through `NewUserStore`, two sessions for `alice` and one for `bob` through `NewSessionStore`, one token each for `alice` and `bob` with `sulis.TokenPurpose("reset")` through `NewTokenStore`, and one token with `UserID: ""`, `Email: "alice@example.test"`, purpose `sulis.TokenPurpose("magic_link")`. Every token hash is distinct. Timestamps are `time.Now().UTC()`, expiries one hour ahead.

```go
func TestDeleteUserRemovesSessionsAndTokens(t *testing.T) {
    testdb.Each(t, func(t *testing.T, db *bun.DB) {
        // seed; users.DeleteUser(ctx, "alice") == nil
        // sessions.ListUserSessions(ctx, "alice") is empty; ListUserSessions(ctx, "bob") has length 1
        // tokens.ConsumeToken(ctx, aliceHash, "reset") returns ErrTokenNotFound
        // tokens.ConsumeToken(ctx, bobHash, "reset") succeeds
        // tokens.ConsumeToken(ctx, magicHash, "magic_link") succeeds: tokens without a user survive
        // users.GetUserByID(ctx, "bob") succeeds
    })
}

func TestDeleteUserMissingAndEmptyID(t *testing.T) {
    testdb.Each(t, func(t *testing.T, db *bun.DB) {
        // seed; DeleteUser(ctx, "nobody") == nil and DeleteUser(ctx, "") == nil
        // afterwards: both users exist; ListUserSessions for alice has length 2, for bob 1;
        // ConsumeToken(ctx, magicHash, "magic_link") succeeds
    })
}

func TestDeleteUserInsideCallerTransaction(t *testing.T) {
    testdb.Each(t, func(t *testing.T, db *bun.DB) {
        // seed through db; tx, _ := db.BeginTx(ctx, nil); txUsers := NewUserStore(tx); txSessions := NewSessionStore(tx)
        // txUsers.DeleteUser(ctx, "alice") == nil
        // through tx: GetUserByID("alice") is ErrUserNotFound and ListUserSessions("alice") is empty
        // tx.Rollback(); through db: GetUserByID("alice") succeeds and ListUserSessions("alice") has length 2
    })
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' go test -count=1 ./auth/bunstore/ -run 'TestDeleteUser'`
Expected: FAIL in `TestDeleteUserRemovesSessionsAndTokens` (alice's sessions and token still present) on both `sqlite` and `postgres`. The other two pass already; they pin behaviour that must not change.

- [ ] **Step 4: Implement `DeleteUser` in `auth/bunstore/user.go`** to the Interfaces block above, with `tx.NewDelete().Model((*sessionModel)(nil))` and the two other models inside the `RunInTx` callback.

- [ ] **Step 5: Run to verify they pass, with the conformance suite**

Run: `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' go test -race -count=1 -v ./auth/bunstore/ 2>&1 | grep -E '^(ok|FAIL|--- (FAIL|SKIP))'`
Expected: `ok`, no `FAIL`, and no `--- SKIP: .*/postgres`.

Run: `go vet ./... && GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' go test -race -count=1 ./...`
Expected: every package `ok`.

- [ ] **Step 6: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/user.go auth/bunstore/user_delete_test.go
git commit -m "Delete a user's sessions and tokens with the user"
```

---

## Done when

Both commits are on `review-follow-ups`, CI is green on its pull request, and `grep -rn '@latest' README.md docs/superpowers/specs/` prints nothing.
