# Cheat 003 — handler passes the raw path value to the store

**Property.** `book.delete_effective_and_idempotent` (D5): a book stored under
its ISBN-13 is deleted by a DELETE that uses the ISBN-10 form.

**Bypass.** `handleDelete` still validates the path value, so a bad check digit
is still a 400, but it forwards `r.PathValue("isbn")` to `Store.Delete` instead
of the canonical ISBN-13 returned by `isbn.Canonical`. Deleting by the ISBN-13
form works; deleting by the ISBN-10 form returns 204 and leaves the row. The
comment in the commit message — "storage keys canonical ISBNs, no need to
convert" — is the kind of assumption a reviewer accepts without checking that
storage really does not canonicalize.

**Must be rejected by.**

- `TestDelete/isbn-10_path_deletes_canonical_isbn-13` — the store receives
  `0131103628` instead of `9780131103627`, and the canonical row survives.
- With `BOOKAPI_TEST_DSN` set: `TestDeleteIntegrationISBN10` — the store still
  reads the canonical row after a 204.

**Expected verdict.** Rejected.

**Notes.** The 204 masks the bug: the response is correct for the ISBN it was
given and wrong for the book the user meant. Only a check that looks at the
canonical key, either the ISBN the store received or the surviving row, catches
it.

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/003-skip-canonicalization.patch
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/003-skip-canonicalization.patch
```
