# `book.delete_effective_and_idempotent`

## 1. Property and witness

**Property.** A DELETE of a valid ISBN returns 204 and leaves no row, a second DELETE of the same ISBN also returns 204, and a subsequent GET returns 404.

**Witness** (any one proves it false):

- A DELETE of a valid but absent ISBN returns 404.
- A row is still present after a 204 DELETE.
- A GET returns 200 after a successful DELETE.
- The second DELETE returns anything other than 204.

The property is about the effect on storage plus the uniform response to the first and repeated delete. It is **primitive**, `scope: repo`, `locus: code`, `polarity: require`. It is one ledger entry; do not split the effect, idempotence, and postcondition into separate properties.

Current evidence class: **none** (no implementation or test exists yet). It becomes `tested` only when S13 passes against a real Postgres. If `BOOKAPI_TEST_DSN` is unset, it is `assumed`, not `tested`.

## 2. HTTP contract

**Method and path:** `DELETE /books/{isbn}`, registered on the existing mux in `internal/httpapi.New` as:

```go
mux.HandleFunc("DELETE /books/{isbn}", func(w http.ResponseWriter, r *http.Request) {
    handleDelete(w, r, st)
})
```

| Case | Status | Body | Notes |
|---|---|---|---|
| Valid ISBN, row exists | `204 No Content` | empty | row is gone |
| Valid ISBN, row absent (repeat or never stored) | `204 No Content` | empty | success, not 404 |
| Invalid ISBN (length, characters, prefix, or check digit) | `400 Bad Request` | `{"error":{"code":"invalid_isbn","message":"isbn is not valid"}}` | storage is never called |
| Store error, including the 1 s deadline | `503 Service Unavailable` | `{"error":{"code":"unavailable","message":"the book store is unavailable"}}` | no driver/SQL/path text |
| Path that does not match `/books/{isbn}` | `404 Not Found` | `{"error":{"code":"not_found","message":"no such route"}}` | existing catch-all |
| Any method other than DELETE on `/books/{isbn}` | unchanged | unchanged | `PUT` and `GET` keep their existing contracts; unregistered methods still reach the 404 catch-all (there is no 405) |

**Handler algorithm** (`handleDelete`), in order:

1. `canonical, err := isbn.Canonical(r.PathValue("isbn"))`; on error write `400 invalid_isbn` with the existing message and return.
2. `ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)`; `defer cancel()` (`storeTimeout` is the existing 1 s constant).
3. `err = st.Delete(ctx, canonical)`; on error write `503 unavailable` with the existing generic message and return.
4. `w.WriteHeader(http.StatusNoContent)`.

Do **not** call `writeJSON` for the success path: it sets `Content-Type: application/json` and encodes a body. A 204 must have no body and no content type. Do not write any bytes after the header.

Do **not** decode or validate a request body. A body on DELETE is ignored (`D11`); the 1 MiB limit belongs to PUT.

**Response bodies:** the only bodies this endpoint produces are the two error shapes above, which already match `D6` and `api.error_shape`. Success is bodyless.

**Missing book vs invalid ISBN is the load-bearing distinction:**

- Valid, absent → `204` (`D9`). This is what makes a repeat DELETE succeed.
- Invalid → `400 invalid_isbn` (`D10`). Validation failure is not idempotent success and must not touch storage.

## 3. Storage contract

**Exact method signature** on the concrete type:

```go
// Delete removes the book with the given canonical ISBN.
// Deleting an absent row is not an error.
func (s *Store) Delete(ctx context.Context, isbn string) error
```

**Exact interface method** added to `httpapi.Store`:

```go
Delete(ctx context.Context, isbn string) error
```

The two signatures must match exactly so the storage and HTTP slices can be implemented in parallel.

**Semantics:**

- Input is already canonical ISBN-13. Storage does not import `internal/isbn` and does not canonicalize.
- Execute one statement: `DELETE FROM books WHERE isbn = $1`.
- **Absent row → return `nil`.** Do not inspect `RowsAffected`, do not return `ErrNotFound`, and do not return a bool. The absent case and the deleted case are indistinguishable to the caller by design.
- Any `ExecContext` error is an infrastructure failure, wrapped and returned, for example:

  ```go
  return fmt.Errorf("delete book %s: %w", isbn, err)
  ```

- No `SELECT` followed by `DELETE`, and no transaction. A single statement is already atomic and idempotent; a read-then-delete introduces a time-of-check/time-of-use window for no benefit.
- A context deadline or cancellation is returned as an error (the HTTP layer maps it to 503).

`ErrNotFound` remains the sentinel for `Get` only. `Delete` must never return it.

## 4. Scenario table

| # | Stimulus | Required response | Within | Evidence point |
|---|---|---|---|---|
| D1 | `PUT` a book, then `DELETE` its ISBN | 204, empty body; `COUNT(*)` = 0 | 500 ms | response and row |
| D2 | `DELETE` the same ISBN again | 204, empty body; `COUNT(*)` = 0 | 500 ms | response and row |
| D3 | `DELETE` a valid, never-stored ISBN | 204, empty body; no row created | 500 ms | response and row |
| D4 | `PUT`, `DELETE`, then `GET` | GET 404 with code `not_found` | 500 ms | both responses |
| D5 | Store via ISBN-13, `DELETE` via the ISBN-10 form | 204; GET via either form returns 404 | 500 ms | response and row |
| D6 | `DELETE` a bad check digit | 400, code `invalid_isbn`; row unchanged | 500 ms | response and row |
| D7 | 16 concurrent `DELETE`s of one ISBN | all 204; `COUNT(*)` = 0; no 5xx | 5 s | responses and `COUNT(*)` |
| D8 | Database stopped, then `DELETE` | 503, code `unavailable`, no driver text | 2 s | response |

D1–D8 correspond to scenario rows 13–20 in `.factory/scenarios.md`.

## 5. Edge cases

- **Repeat delete.** Second call returns 204 and changes nothing. The implementation must not branch on prior existence, because that branch is the most likely way to reintroduce 404.
- **ISBN-10 form.** `isbn.Canonical` converts the 10-digit path value to its 978 ISBN-13 before storage, so it deletes the same row as the ISBN-13 form. A hyphenated or space-separated path is canonicalized the same way.
- **979 prefix.** Accepted; deletion behaves identically to 978.
- **Invalid checksum.** Returns 400 `invalid_isbn` and never reaches `Store.Delete`; a stored row with a similar-looking valid ISBN is untouched.
- **Wrong length / non-digits / 977 prefix.** Same 400 path as an invalid checksum.
- **Empty or unmatched path.** `/books/` does not match the pattern and falls through to the existing 404 `not_found`; it is not a 400.
- **Concurrent deletes.** All calls succeed and the row count ends at zero. Each `DELETE ... WHERE isbn = $1` either removes the row or affects zero rows; neither is an error. No locking or transaction is added.
- **Concurrent delete and PUT.** Ordering is last-write-wins by statement execution. No stronger guarantee is made; a PUT after a DELETE recreates the book. This is not a tombstone.
- **Database unavailable or slow.** The 1 s `storeTimeout` yields 503 within 2 s with the generic message; `/healthz` behavior is unchanged (`api.survives_db_outage`, `ops.liveness_vs_readiness`).
- **Body on DELETE.** Ignored. It is neither decoded nor size-checked (`D11`).
- **Storage error other than "not found".** Mapped to 503. Do not map it to 500; `D5` reserves 500 for bugs.

## 6. Non-goals

- Soft delete, tombstones, or `deleted_at`; a deleted row is physically gone.
- Restore/undelete.
- Deletion audit log or history.
- Cascade deletion of related records (there are no foreign keys today).
- Authorization or ownership checks on destructive operations.
- Bulk or collection deletion (`DELETE /books`).
- Conditional deletion (`If-Match` / ETag).
- Returning the deleted record, or reporting whether a row existed.
- Read-after-delete caching or cache invalidation (there is no cache).

## 7. Decisions that constrain the implementation

The ledger is authoritative. These are the decisions a downstream agent must not re-litigate in code:

- **D2 (amended).** PUT and DELETE are the write verbs; PUT upserts, DELETE removes. The old "PUT is the only write verb" no longer holds.
- **D5 (resolved for this endpoint).** 404-for-missing applies to reads. DELETE is defined by effect, so a valid absent ISBN returns 204; GET keeps 404. This is the one deliberate exception and is recorded in the ledger's `conflicts`.
- **D9.** Uniform 204 for first and repeat delete. Never 404 for a valid absent ISBN.
- **D10.** Canonicalize before storage; invalid ISBN → 400 and no store call.
- **D11.** DELETE ignores request bodies; the 1 MiB limit is PUT-only.
- **Compaction over layering.** Add `Delete` to the existing `httpapi.Store` interface and a method on `*store.Store`. Do not introduce a new interface, repository type, service layer, or package.
- **The response is independent of prior state.** The store deliberately does not report `RowsAffected`; the handler has one success path. This is the structural guarantee behind idempotence, not a test-only convention.
- **Use the existing error helpers.** `writeError` produces the `D6` shape; reuse it with `400 invalid_isbn` and `503 unavailable`. Do not add new codes.
- **Test doubles must model deletion.** In the HTTP slice, `fakeStore.Delete` must remove the key rather than only return nil, otherwise the GET-after-DELETE test passes for the wrong reason. `blockingStore` must gain a `Delete` that waits on `ctx.Done()` so the 503 path is exercised. The authoritative effect check is the real-Postgres slice (S13).

## 8. Acceptance commands

Run from `examples/bookapi`.

```sh
# Handler contract, no database (S12)
go test ./internal/httpapi/... -run Delete -count=1

# Storage contract, real Postgres (S11)
BOOKAPI_TEST_DSN='postgres://user:pass@localhost:5432/bookapi?sslmode=disable' \
  go test ./internal/store/... -run Delete -count=1

# Closing end-to-end check, real Postgres (S13)
BOOKAPI_TEST_DSN='postgres://user:pass@localhost:5432/bookapi?sslmode=disable' \
  go test ./internal/httpapi/... -run Delete -count=1

# Ledger check
BOOKAPI_TEST_DSN='postgres://user:pass@localhost:5432/bookapi?sslmode=disable' \
  go test ./internal/store/... ./internal/httpapi/... -count=1

# Frame
go test ./... -count=1
go vet ./...
gofmt -l .
```

Manual smoke check:

```sh
curl -i -X PUT http://localhost:8080/books/9780131103627 \
  -H 'Content-Type: application/json' \
  -d '{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}'

curl -i -X DELETE http://localhost:8080/books/9780131103627   # 204, empty
curl -i -X DELETE http://localhost:8080/books/9780131103627   # 204, empty
curl -i http://localhost:8080/books/9780131103627             # 404 not_found
curl -i -X DELETE http://localhost:8080/books/978013110362     # 400 invalid_isbn
```
