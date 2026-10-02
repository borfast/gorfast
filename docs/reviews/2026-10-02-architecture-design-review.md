# Architecture and design review

**Date:** 2026-10-02
**Scope:** Review of the documented design and plans. The implementation was not verified and tests were not rerun for this review.

Documents reviewed:

- [Gorfast design](../superpowers/specs/2026-09-27-gorfast-design.md)
- [Crypt design](../superpowers/specs/2026-10-02-crypt-design.md)
- [Bunstore core implementation plan](../superpowers/plans/2026-09-27-bunstore-core.md)
- [PR #1 fixes implementation plan](../superpowers/plans/2026-09-28-pr-1-fixes.md)

## Overall assessment

The overall architecture is strong and worth keeping. The strongest decisions are explicit composition in `main`, separate domain and persistence models, small backend contracts, and real-database conformance tests. The biggest gaps concern how those pieces compose: transaction ownership, the boundary around Bun, and operational behavior during encryption rotation.

## 1. Clarify the dependency rule

The Gorfast design's doctrine prohibits third-party types in every public API, but the stores deliberately accept `bun.IDB`, and the proposed Postgres queue does too. The implementation plan introduces an exception for `auth/bunstore` without resolving the broader rule.

The constructors are sensible. Change the rule to: **domain and service contracts remain dependency-independent; explicitly named adapters may expose their underlying dependencies when being constructed.** That preserves replaceability without forcing a second database abstraction.

Likewise, keep the prohibition on feature-to-feature imports, but demonstrate where integration belongs. Wiring transactions, CSRF, forms, and authentication together will test that boundary much more than isolated packages will.

## 2. Define transaction semantics before making transaction-per-request a default

Accepting `bun.IDB` is a good transaction seam. It does not yet establish that transaction-per-request is the right lifecycle.

Consider a failed login: if the request records a failed attempt and then returns an authentication error, rolling back on every error could erase the security bookkeeping. Conversely, committing after a handler writes its response can leave the client seeing success when the commit fails.

Make the service operation the primary transaction boundary, with explicit decisions about which effects survive failure. HTTP middleware can provide convenience once those semantics are settled. Email and other external effects also need an explicit relationship to commit.

## 3. Treat conformance suites as a foundation, not the entire correctness criterion

The original Bunstore plan treats Sulis's suite as the correctness criterion. The repair plan usefully demonstrates the additional concerns: driver-specific errors, transaction usability, cleanup, and dependency reproducibility.

Explicitly require both contract conformance and adapter-specific integration tests. For Postgres support, CI should require a Postgres run even though local runs may skip it.

There is also an omission in `crypt`: it defines `Encryptor` and `KeySource`, but its package layout and test plan do not include the public conformance suites the doctrine promises. Separate portable interface behavior from Keyring-specific envelope tests.

## 4. Tighten the crypt API semantics

The `crypt` design is thoughtfully bounded. Explicit sealing during model conversion, authenticated headers, unambiguous context encoding, and staged key rollout are all good choices. Dropping automatic encryption inside SQL column types makes ownership much clearer.

Before implementing the API, settle these details:

- `Reseal` should authenticate even when the header identifies the current key. Otherwise, a shortcut could return success for tampered ciphertext or the wrong context.
- `NeedsReseal` needs defined behavior for malformed values, unknown keys, and zero values. A boolean alone cannot distinguish every case; document it as an inspection hint if that is the intent.
- Third-party `Encryptor` implementations need a deliberate way to construct and inspect `Sealed`. Currently they would seemingly have to use SQL `Scan`/`Value` as a byte interchange API.
- "Arbitrary input ... never succeeds" is an incorrect fuzzing invariant: arbitrary input includes valid ciphertext. Require no panics, correct acceptance of valid values, and rejection of specified invalid transformations.

Also, turn the AES-GCM usage limit into an operational requirement. Go documents a maximum of 2^32 messages per key with random nonces; the relevant budget spans every instance using that key. See the [Go cipher documentation](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce).

## 5. Give key rotation a persistence protocol

The two-stage rollout handles mixed application configurations well. However, a background resealing loop can overwrite a concurrent application edit unless its write checks the original ciphertext or row version.

Document conditional updates, retries, and waiting for all old-key writers to finish before declaring migration complete. Removing a key also needs to account for retained backups, not just current rows.

Context binding deserves a short application contract too: use stable identifiers, include tenant identity where IDs are tenant-local, and explain that renaming a context component requires migration. Binding to a location does not by itself prevent replaying an older ciphertext at that same location.

## 6. Define job failure semantics before promising equivalent backend behavior

Capability declarations and transactional Postgres enqueue are good foundations. But putting retry policy in one worker loop does not alone make backend behavior identical.

The eventual contract needs to describe delivery guarantees, worker crashes, durable retry state, acknowledgement failures, and idempotency. RabbitMQ explicitly documents redelivery and recommends idempotent consumers. See the [RabbitMQ reliability guide](https://www.rabbitmq.com/docs/reliability).

Implement Postgres first and derive the portable contract from demonstrated requirements before committing to the other adapters.

## 7. Bring the reference application forward and mark superseded instructions

The sequence delays the example until several packages exist. Grow one small application alongside them. A signed-in operation involving a transaction and an encrypted field would expose composition problems early.

The older Bunstore plan also still says second-factor stores require `crypt` and insists on a local Sulis replacement. Newer documents supersede both decisions. Preserve the history, but add explicit superseded notices so another agent cannot mistake those instructions for current requirements.

## Recommended priorities

1. Resolve the adapter boundary and transaction semantics.
2. Tighten the `crypt` contract and rotation guidance.
3. Use the example application to guide the next packages.

The existing package structure gives a good basis for doing this without a broad redesign.
