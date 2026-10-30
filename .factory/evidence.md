# Evidence

Recorded 2026-10-29. Every property below is `tested` on the regenerated
implementation. The implementation was regenerated from the ledger, contract,
scenarios, and property files; the property statements, witnesses, polarity,
contract, and scenarios were not changed.

## Environment

| Item | Value |
|---|---|
| Go | go1.27.2 darwin/arm64 |
| Database | PostgreSQL 16.15 (Homebrew), local cluster on `127.0.0.1:55432`, database `bookapi` |
| `BOOKAPI_TEST_DSN` | `postgres://madxmike@127.0.0.1:55432/bookapi?sslmode=disable` |
| Package layout | `book`, `isbn`, `store`, `httpapi`, `cmd/api`; no `internal` directory |

Saved artifacts:

- `.factory/evidence/suite-no-dsn.txt` — `go test ./... -count=1` with no DSN.
- `.factory/evidence/suite-with-postgres.txt` — `BOOKAPI_TEST_DSN=... go test ./... -count=1`.
- `.factory/evidence/suite-with-postgres-race.txt` — `BOOKAPI_TEST_DSN=... go test -race ./... -count=1`.
- `.factory/evidence/vet.txt` — `go vet ./...`.
- `.factory/evidence/gofmt.txt` — `gofmt -l` output (empty).
- `.factory/evidence/fuzz-isbn.txt` — 10-second fuzz run.
- `.factory/evidence/outage.log` — live server with PostgreSQL stopped and restarted.
- `.factory/cheats/book.delete_effective_and_idempotent/results.md`
- `.factory/cheats/code.no_internal_package/results.md`
- `.factory/cheats/isbn.canonical/results.md`
- `.factory/cheats/write.idempotent/results.md`
- `.factory/cheats/api.error_shape/results.md`
- `.factory/evidence/mutations/` — logs from the three verifier-gap mutation runs.

## Property evidence

| Property | Class | Artifact | Assumption |
|---|---|---|---|
| `isbn.canonical` | tested | `isbn/isbn_test.go:TestCanonical` (30 cases: ISBN-10/13 equivalence, `X`, separators, bad check digits, and checksum-valid `9770131103628` / `977-0-13-110362-8` rejected only by the 978/979 prefix rule); `FuzzCanonicalIdempotent` (5,869,809 execs in the saved 10-second run); `httpapi/httpapi_test.go:TestHandlers/get_stored_book_via_isbn-10`, `TestHandlers/get_977_prefix_with_valid_checksum`, `TestPutStoresCanonicalISBN`; `httpapi/integration_test.go:TestGetViaISBN10`, `TestDeleteIntegrationISBN10`; mutation `001-no-prefix-guard` rejected | none |
| `book.persisted` | tested | `cmd/api/restart_integration_test.go:TestProcessRestartPersistsBook` (builds the binary, writes through one process, stops it, starts a second process, reads the row); `httpapi/integration_test.go:TestPutGetAcrossInstances`, `TestPutGetAcrossRestart`; `store/store_test.go:TestPutGetRoundTrip`, `TestCloseAndReopenStillReads`, `TestTwoStoresSeeEachOthersWrites` | `postgres_enforces_primary_key_under_concurrency` |
| `write.idempotent` | tested | `store/store_test.go:TestPutIdenticalTwiceKeepsOneRow`, `TestPutTwiceLastWriteWins`; `httpapi/integration_test.go:TestPutIsIdempotentAndLastWriteWins` (identical PUTs produce the same stored record and one row; after an update it reads the row back through `Store.Get` and requires the updated fields); mutation `001-upsert-does-nothing` rejected | none |
| `write.no_duplicate_under_concurrency` | tested | `store/store_test.go:TestConcurrentPutsKeepOneRow` (32 writers, `COUNT(*) = 1`); `httpapi/integration_test.go:TestConcurrentPutsOneRow` (32 concurrent HTTP PUTs, all 2xx, `COUNT(*) = 1`) | `postgres_enforces_primary_key_under_concurrency` |
| `api.read_shape` | tested | `httpapi/httpapi_test.go:TestHandlers` (200 with fields, 404 `not_found`, 400 `invalid_isbn`, GET via ISBN-10); `httpapi/integration_test.go:TestDeleteIntegrationEffectAndIdempotence` (GET after delete is 404 `not_found`) | none |
| `api.error_shape` | tested | `httpapi/httpapi_test.go:assertErrorEnvelope` used by `TestHandlers`, `TestDelete/store_error_returns_503_without_leaking_detail`, and `TestStoreTimeoutReturns503`: exactly one `error` object, exactly `code` and `message` strings, and a message from the fixed set for that code; `httpapi/integration_test.go:requireErrorEnvelope` does the same for DB-backed errors; mutation `001-stack-trace-in-503` rejected; `.factory/evidence/outage.log` (live outage responses contained only the stable `unavailable` body) | none |
| `api.survives_db_outage` | tested | `httpapi/httpapi_test.go:TestStoreTimeoutReturns503` (GET, PUT, DELETE return 503 `unavailable` within 2 s against a store that blocks until the deadline); `cmd/api/main_test.go:TestLivenessAndReadiness`, `TestHealthzNeverConsultsStorage`; `.factory/evidence/outage.log` (live server: GET/PUT/DELETE 503 `unavailable` in about 1 ms, `/healthz` 200, `/readyz` 503) | none |
| `ops.liveness_vs_readiness` | tested | `cmd/api/main_test.go:TestLivenessAndReadiness` (healthz 200 with a failing ready function; readyz 503); `TestHealthzNeverConsultsStorage`; `.factory/evidence/outage.log` (healthz 200, readyz 503 during the outage) | none |
| `api.body_limit` | tested | `httpapi/httpapi_test.go:TestHandlers/put_oversized_body`, `/put_oversized_trailing_body` (2 MiB -> 413 `payload_too_large`); `httpapi/integration_test.go:TestBodyLimitIntegration` (2 MiB -> 413 within 1 s) | none |
| `book.delete_effective_and_idempotent` | tested | `httpapi/httpapi_test.go:TestDelete` (effect, repeat delete, absent delete, 404 after delete, invalid ISBN does not reach storage, 204 has no body or content type, ISBN-10 form, ignored request body, store error is 503); `httpapi/integration_test.go:TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationAbsent`, `TestDeleteIntegrationISBN10`, `TestDeleteIntegrationInvalidLeavesRow`, `TestDeleteIntegrationConcurrent` (all assert `COUNT(*)` through the store); `.factory/cheats/book.delete_effective_and_idempotent/results.md` (six reintroductions rejected) | `postgres_delete_where_missing_commits_successfully` |
| `code.no_internal_package` | tested | `cmd/api/structure_test.go:TestNoInternalPackage` (walks the module, skips `.git`, rejects directories and symlinks named exactly `internal` at any depth, and names each violating path); `.factory/cheats/code.no_internal_package/results.md` | none |

## Verifier gap closures

Each fix is backed by a mutation that the strengthened check rejects.

| Gap | Mutation | Patch | Observed rejection |
|---|---|---|---|
| `isbn.canonical` prefix rule was masked by bad checksums | remove the 978/979 prefix guard | `.factory/cheats/isbn.canonical/001-no-prefix-guard.patch` | `TestCanonical/977_prefix_with_valid_checksum`, `TestCanonical/977_prefix_with_valid_checksum_and_hyphens`, `TestHandlers/get_977_prefix_with_valid_checksum`; after reverse, green |
| `write.idempotent` HTTP test did not read storage | change the upsert to `ON CONFLICT ... DO NOTHING` | `.factory/cheats/write.idempotent/001-upsert-does-nothing.patch` | `TestPutIsIdempotentAndLastWriteWins` fails on the stored record (original title/year, not the update); `TestPutTwiceLastWriteWins` also fails; after reverse, green |
| `api.error_shape` only grepped four substrings | append `debug.Stack()` to every 503 message | `.factory/cheats/api.error_shape/001-stack-trace-in-503.patch` | `TestHandlers/get_store_error`, `TestHandlers/put_store_error`, `TestStoreTimeoutReturns503/{GET,PUT,DELETE}`, `TestDelete/store_error_returns_503_without_leaking_detail`; the injected text has none of the four old substrings; after reverse, green |

Full logs: `.factory/evidence/mutations/`.

The ISBN fixtures are now checksum-valid 977 values (`9770131103628` and
`977-0-13-110362-8`), and `TestHandlers/get_977_prefix_with_valid_checksum`
asserts the HTTP-level 400 `invalid_isbn` response.

## Delete negative controls

All six reintroductions are rejected with the DSN set; details and failing test
names are in `.factory/cheats/book.delete_effective_and_idempotent/results.md`.

- `001-delete-noop` — 204 without touching storage.
- `002-delete-absent-404` — 404 for a valid absent ISBN.
- `003-skip-canonicalization` — raw path value reaches the store.
- `004-body-on-204` — 204 carries a JSON body and content type.
- `005-swallow-store-error` — storage failure is reported as 204.
- `006-store-reports-absent` — store returns `ErrNotFound` when no row was
  deleted; the handler still answers 204, but the store contract tests reject
  the change.

The first five are the reintroductions named in the property's evidence
requirements. The checks that reject them read storage, not just status codes:
`COUNT(*)` and `Store.Get` are asserted after the HTTP response.

## `code.no_internal_package` negative controls

`TestNoInternalPackage` passes on the regenerated tree. Reintroducing the
violation makes it fail and name the path:

- top-level `internal/keep.go` -> `forbidden internal package: internal`;
- nested `httpapi/internal/x.go` -> `forbidden internal package: httpapi/internal`;
- a symlink named `internal` -> `forbidden internal package: internal`.

The check stays green for a regular file named `internal`, and for directories
named `Internal` and `.internal`, matching the property's definition. Reversing
each control restores a green run. See
`.factory/cheats/code.no_internal_package/results.md`.

## Live outage run

`.factory/evidence/outage.log` records a real process against PostgreSQL 16.15:

1. Start the server and PUT `9780306406157` (200).
2. Stop PostgreSQL with `pg_ctl -m fast stop`.
3. `GET`, `PUT`, and `DELETE` return 503 `unavailable` in about 1 ms, with no
   driver or SQL text.
4. `/healthz` returns 200; `/readyz` returns 503.
5. Restart PostgreSQL; `/readyz` returns 200 and DELETE of the test row returns
   204.

This supplements the automated handler tests; it is a manual run, not part of
`go test ./...`.

## Corner not covered

- `api.body_limit` is enforced in the handler. A proxy that buffers or rewrites
  bodies in front of this service must be re-tested at that boundary.
- The restart test stops and starts the server process, not the host; durability
  across a host reboot is PostgreSQL's responsibility.
- Concurrency evidence uses 32 concurrent PUTs and 16 concurrent DELETEs. It
  exercises the real primary-key and upsert behavior but does not enumerate
  every interleaving.
- `code.no_internal_package` checks directory and symlink names. It does not
  inspect file contents, because the property is structural.
