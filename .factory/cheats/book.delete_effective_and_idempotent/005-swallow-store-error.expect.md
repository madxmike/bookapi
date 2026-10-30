# Cheat 005 — the handler swallows the store error

**Property.** `book.delete_effective_and_idempotent`: a store failure is
reported as 503 `unavailable`; only a successful delete is a 204.

**Bypass.** The handler discards the error from `st.Delete` and always writes
204. The success path, the repeat path, and the canonicalization are unchanged.
The patch looks like the "absent row is not an error" rule taken one step
further: if deletion is best-effort and idempotent, why fail the request? A
reviewer could accept it as deliberate fire-and-forget cleanup.

This cheat does not violate the property's four witnesses directly; it violates
the endpoint contract that makes those witnesses meaningful, because it turns a
dead database into a success.

**Must be rejected by.**

- `TestDelete/store_error_returns_503_without_leaking_detail`.
- `TestStoreTimeoutReturns503/DELETE` — the blocking store's deadline is
  reported as 204 instead of 503.
- With `BOOKAPI_TEST_DSN` set: nothing extra. The integration test uses a
  healthy database, so it cannot produce a store error; the fake and the
  blocking store own this path.

**Expected verdict.** Rejected.

**Notes.** The only case where the fake-store suite is strictly stronger than
the real-Postgres suite, because a healthy database cannot be told to fail.
Removing either suite would lose a distinct case.

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/005-swallow-store-error.patch
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/005-swallow-store-error.patch
```
