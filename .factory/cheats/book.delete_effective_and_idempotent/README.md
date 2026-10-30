# Cheat corpus — `book.delete_effective_and_idempotent`

Adversarial patches for property
`book.delete_effective_and_idempotent`: a DELETE of a valid ISBN returns 204,
leaves no row, a repeat DELETE also returns 204, and a following GET is 404.

Each patch is minimal, compiles, and keeps unrelated tests green. Each is a
change a reviewer could plausibly accept. Applying one must make the suite
fail; if it does not, the property is not actually tested.

| Patch | Bypass | Verdict |
|---|---|---|
| `001-delete-noop.patch` | 204 without calling `Delete` | rejected |
| `002-delete-absent-404.patch` | `Delete` returns `ErrNotFound` on zero rows, so a repeat DELETE fails | **accepted without a database** — see `results.md` |
| `003-skip-canonicalization.patch` | raw path value reaches the store, so the ISBN-10 form deletes nothing | rejected |
| `004-body-on-204.patch` | success path uses `writeJSON`, adding a body and a content type | rejected |
| `005-swallow-store-error.patch` | store error is discarded and reported as 204 | rejected |

Each cheat has a matching `<name>.expect.md` with the property, the bypass, the
test that must reject it, the expected verdict, and notes. Observed runs are in
`results.md`.

## Running the corpus

Apply from the repository root; the patch paths are repository-root relative.

```sh
# from the repository root
git apply examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch

# from examples/bookapi
go test ./internal/httpapi/... -count=1

# from the repository root
git apply -R examples/bookapi/.factory/cheats/book.delete_effective_and_idempotent/001-delete-noop.patch
```

Run the corpus against a live database as well:

```sh
# from examples/bookapi
BOOKAPI_TEST_DSN='postgres://user:pass@localhost:5432/bookapi?sslmode=disable' \
  go test ./internal/httpapi/... -run Delete -count=1
```

Without `BOOKAPI_TEST_DSN` the S13 integration tests skip, and cheat 002 slips
through. That is the known gap, documented in `results.md` and in
`002-delete-absent-404.expect.md`.
