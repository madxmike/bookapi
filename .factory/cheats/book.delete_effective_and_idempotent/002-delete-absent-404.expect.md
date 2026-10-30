# Cheat 002 — store reports a missing row as `ErrNotFound`

**Property.** `book.delete_effective_and_idempotent`: a repeat DELETE of the
same ISBN returns 204. An absent row is a success, not an error.

**Bypass.** `Store.Delete` inspects `RowsAffected` and returns `ErrNotFound`
when the statement deleted nothing. The handler is unchanged, so its error path
maps `ErrNotFound` to 503. The first DELETE of an existing book is correct; only
the second one fails. The patch reads like careful bookkeeping — "tell the
caller whether anything was deleted" — and it keeps `Get`'s sentinel semantics
looking consistent, which is what makes it tempting.

**Must be rejected by.**

- With `BOOKAPI_TEST_DSN` set:
  `TestDeleteIntegrationEffectAndIdempotence` — the second DELETE returns 503
  and the handler-level GET check never runs.
- With `BOOKAPI_TEST_DSN` set: `TestDeleteAbsentReturnsNil` and
  `TestDeleteTwiceReturnsNil` in `internal/store`.
- Without a DSN: **nothing.** The fake store in `httpapi_test.go` never returns
  `ErrNotFound` from `Delete`, so no handler test can observe this bug, and
  every Postgres test skips.

**Expected verdict.** Rejected with a database; **accepted without one.**

**Notes.** This is the documented evidence gap. The response contract and the
storage contract are only joined by a real database, so in an environment with
no `BOOKAPI_TEST_DSN` this cheat passes the whole suite. The property is
`assumed`, not `tested`, in that environment; `results.md` records it. The test
was not weakened to force a rejection, because a fake store returning
`ErrNotFound` would model the *bug* rather than the contract, and the store
side is `internal/store`'s scope, not S13's.

Do not "fix" this by making `fakeStore.Delete` return `ErrNotFound` for a
missing key: that would make the S12 tests assert the wrong contract and would
mask a real regression in the opposite direction.

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/002-delete-absent-404.patch
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/002-delete-absent-404.patch
```
