# Cheat 001 — delete is a no-op that answers 204

**Property.** `book.delete_effective_and_idempotent`: a DELETE of a valid ISBN
returns 204 and leaves no row, a repeat DELETE also returns 204, and a
subsequent GET returns 404.

**Bypass.** `handleDelete` still validates the path value, so the invalid-ISBN
case looks right, but it never calls `st.Delete`. Every DELETE is acknowledged
as a success without touching storage. A reviewer could accept this while
reading only status codes, because the response for a never-stored book is
supposed to be identical to the response for a deleted one.

**Must be rejected by.**

- `TestDelete/existing_book_returns_204_with_empty_body` — the row is still in
  the store after a 204.
- `TestDelete/get_after_delete_returns_404` — GET returns 200 with the book.
- `TestDelete/isbn-10_path_deletes_canonical_isbn-13` — the store is never
  called.
- `TestDelete/store_error_returns_503_without_leaking_detail`.
- `TestStoreTimeoutReturns503/DELETE` — a dead database still yields 204.
- With `BOOKAPI_TEST_DSN` set:
  `TestDeleteIntegrationEffectAndIdempotence` — the store still reads the row.

**Expected verdict.** Rejected. The response is uniform but the effect is
missing; status codes alone cannot distinguish this from the real endpoint,
which is exactly why the checks must read storage.

**Notes.** The most plausible cheat in the corpus: it satisfies every
response-shaped assertion while violating the point of the property.

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch
```
