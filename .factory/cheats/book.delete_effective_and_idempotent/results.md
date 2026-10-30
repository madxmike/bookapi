# Results — S14 cheat corpus for `book.delete_effective_and_idempotent`

## Environment

| Item | Value |
|---|---|
| Date | 2026-10-29 |
| Go | go1.27.2 darwin/arm64 |
| `BOOKAPI_TEST_DSN` | unset (no Postgres available on this host: no `psql`, no Postgres.app, no container runtime) |
| Working directory for `go test` | `examples/bookapi` |
| Working directory for `git apply` | repository root |

Because no database was reachable, the S13 integration tests
(`TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationISBN10`,
`TestDeleteIntegrationConcurrent`) skipped in every run below. Against a real
Postgres the corpus is stronger than what is recorded here; see the gap in
result 002.

Every cheat was applied with `git apply` from the repository root and reverted
with `git apply -R`, and the suite was confirmed green after each revert.

## Procedure

Per cheat:

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/<cheat>.patch

# from examples/bookapi
go test ./internal/httpapi/... -count=1

# from the repository root
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/<cheat>.patch

# from examples/bookapi — green again
go test ./internal/httpapi/... -count=1
```

## Verdicts

| # | Cheat | Verdict | Failing test(s) |
|---|---|---|---|
| 001 | `001-delete-noop.patch` | Rejected | `TestDelete/existing_book_returns_204_with_empty_body`, `TestDelete/get_after_delete_returns_404`, `TestDelete/isbn-10_path_deletes_canonical_isbn-13`, `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503/DELETE` |
| 002 | `002-delete-absent-404.patch` | **Accepted** (no DSN) | none without a DSN |
| 003 | `003-skip-canonicalization.patch` | Rejected | `TestDelete/isbn-10_path_deletes_canonical_isbn-13` |
| 004 | `004-body-on-204.patch` | Rejected | `TestDelete/existing_book_returns_204_with_empty_body`, `TestDelete/repeat_delete_returns_204`, `TestDelete/request_body_is_ignored` |
| 005 | `005-swallow-store-error.patch` | Rejected | `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503/DELETE` |

### 001 — delete-noop

```text
--- FAIL: TestStoreTimeoutReturns503 (2.00s)
    --- FAIL: TestStoreTimeoutReturns503/DELETE (0.00s)
        httpapi_test.go:340: status = 204, want 503
--- FAIL: TestDelete (0.00s)
    --- FAIL: TestDelete/existing_book_returns_204_with_empty_body (0.00s)
        httpapi_test.go:396: book "9780131103627" still in store after delete
    --- FAIL: TestDelete/get_after_delete_returns_404 (0.00s)
        httpapi_test.go:425: get: status = 200, want 404 (body "{\"isbn\":\"9780131103627\",...}")
    --- FAIL: TestDelete/store_error_returns_503_without_leaking_detail (0.00s)
        httpapi_test.go:457: status = 204, want 503 (body "")
    --- FAIL: TestDelete/isbn-10_path_deletes_canonical_isbn-13 (0.00s)
        httpapi_test.go:480: store received isbn "", want canonical "9780131103627"
        httpapi_test.go:483: book "9780131103627" still in store after delete
```

The effect check is what rejects it. `TestDelete/repeat_delete_returns_204`
still passes, which is the point: a uniform 204 is compatible with doing
nothing at all.

### 002 — delete-absent-404 — ACCEPTED, gap

Patch applied, then:

```text
ok  	example.com/bookapi/internal/httpapi	3.205s
```

The only tests that would fail are the ones that need a database:
`TestDeleteIntegrationEffectAndIdempotence` (S13) and the store's
`TestDeleteAbsentReturnsNil` / `TestDeleteTwiceReturnsNil`, all skipped:

```text
=== RUN   TestDeleteAbsentReturnsNil
    store_test.go:190: BOOKAPI_TEST_DSN is not set; skipping Postgres integration test
--- SKIP: TestDeleteAbsentReturnsNil (0.00s)
```

**Gap.** With `BOOKAPI_TEST_DSN` unset, the repeat-DELETE half of the property
is unobservable. `fakeStore.Delete` returns `nil` unconditionally, by design:
the handler contract says a store failure is a 503, and the fake must model the
*contract*, not the bug. Making the fake return `ErrNotFound` for a missing key
would invert the meaning of the S12 tests instead of catching regressions.

**Not fixed here on purpose.** The test was not weakened or extended to force
this cheat to fail:
- S13's scope is the handler plus the real store; the store-only contract
  belongs to `internal/store`, whose tests already cover it and already skip
  without a DSN.
- `internal/store/store_test.go` is outside this slice's write scope and is not
  modified.

**Consequence for the ledger.** Until `go test ./internal/httpapi/... -run
Delete` is run with a live `BOOKAPI_TEST_DSN`, this property's evidence class
is `assumed`, not `tested`. While it is `assumed`, cheat 002 is a live
regression that the default `go test ./...` cannot see.

**Closing this gap is not a test change.** The environment needs a Postgres, or
the property needs an explicit `assumed` marker in the ledger. Adding a
`syntax-only` shortcut such as a fake that inspects a map to decide whether to
return `ErrNotFound` would encode the bug and should be rejected in review.

### 003 — skip-canonicalization

```text
--- FAIL: TestDelete (0.00s)
    --- FAIL: TestDelete/isbn-10_path_deletes_canonical_isbn-13 (0.00s)
        httpapi_test.go:480: store received isbn "0131103628", want canonical "9780131103627"
        httpapi_test.go:483: book "9780131103627" still in store after delete
```

Two independent observations fail: the key the store received and the row that
survived.

### 004 — body-on-204

```text
--- FAIL: TestDelete (0.00s)
    --- FAIL: TestDelete/existing_book_returns_204_with_empty_body (0.00s)
        httpapi_test.go:390: body = "{\"status\":\"deleted\"}\n", want empty
        httpapi_test.go:393: Content-Type = "application/json", want none
    --- FAIL: TestDelete/repeat_delete_returns_204 (0.00s)
        httpapi_test.go:410: delete 1: body = "{\"status\":\"deleted\"}\n", want empty
        httpapi_test.go:410: delete 2: body = "{\"status\":\"deleted\"}\n", want empty
    --- FAIL: TestDelete/request_body_is_ignored (0.00s)
        httpapi_test.go:496: body = "{\"status\":\"deleted\"}\n", want empty
```

### 005 — swallow-store-error

```text
--- FAIL: TestStoreTimeoutReturns503 (3.00s)
    --- FAIL: TestStoreTimeoutReturns503/DELETE (1.00s)
        httpapi_test.go:340: status = 204, want 503
--- FAIL: TestDelete (0.00s)
    --- FAIL: TestDelete/store_error_returns_503_without_leaking_detail (0.00s)
        httpapi_test.go:457: status = 204, want 503 (body "")
```

Note the 1 s delay on the deadline case: the handler still waits for the store,
it just discards the result.

## Tree state

After the last revert:

```text
?? examples/bookapi/.factory/cheats/
?? examples/bookapi/internal/httpapi/delete_integration_test.go
```

No production file is left modified; `git checkout` / `git apply -R` restored
each patched file and `git --no-optional-locks status --short` lists only the
new files.
