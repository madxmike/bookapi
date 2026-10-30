# Cheat corpus: `book.delete_effective_and_idempotent`

Each patch is a minimal, compiling reintroduction of a defect the property
forbids. Apply from the repository root, run the suite, then reverse the patch.

| Patch | Reintroduction | Expected |
|---|---|---|
| `001-delete-noop.patch` | Returns 204 without calling the store | rejected |
| `002-delete-absent-404.patch` | Pre-reads storage and returns 404 for an absent ISBN | rejected |
| `003-skip-canonicalization.patch` | Passes the raw path value to the store | rejected |
| `004-body-on-204.patch` | Writes a JSON body and content type on 204 | rejected |
| `005-swallow-store-error.patch` | Ignores the store error and returns 204 | rejected |
| `006-store-reports-absent.patch` | Store returns `ErrNotFound` when no row was deleted | rejected |

The first five are the reintroductions named in the property's evidence
requirements. `006` additionally pins the storage contract that makes the
handler's 204 decision safe.

## Run

```sh
git apply .factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch
BOOKAPI_TEST_DSN='postgres://madxmike@127.0.0.1:55432/bookapi?sslmode=disable' \
  go test ./... -count=1
git apply -R .factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch
```

Observed results are recorded in `results.md` and summarized in
`.factory/evidence.md`.
