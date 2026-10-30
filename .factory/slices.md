# Slices

One property per slice. Each slice has a verification command and a frame.

| # | Slice | Property | Owner scope | Verify |
|---|---|---|---|---|
| S00 | Module, ledger, scenarios | harness | `go.mod`, `.factory/**` | `go build ./...` |
| S01 | Server skeleton: `/healthz`, `/readyz`, graceful shutdown | `ops.liveness_vs_readiness` (partial) | `cmd/api/**`, `Makefile`, `README.md` | `go test ./cmd/api/...` |
| S02 | ISBN canonicalization | `isbn.canonical` | `internal/isbn/**` | `go test ./internal/isbn/...` |
| S03 | Domain type | — | `internal/book/**` | `go build ./internal/book/...` |
| S04 | Postgres store and migration | `book.persisted`, `write.idempotent`, `write.no_duplicate_under_concurrency` | `internal/store/**` (including `internal/store/migrations/`), `go.mod`, `go.sum` | `go test ./internal/store/...` |
| S05 | HTTP handlers: PUT and GET | `api.read_shape` | `internal/httpapi/**` | `go test ./internal/httpapi/...` |
| S06 | Wire store and handlers into `cmd/api` | `book.persisted` (end to end) | `cmd/api/**` | `go test ./cmd/api/...` |
| S07 | Error mapping and database outage | `api.error_shape`, `api.survives_db_outage` | `internal/httpapi/**`, `cmd/api/**` | `go test ./internal/httpapi/... ./cmd/api/...` |
| S08 | Body limit and content type | `api.body_limit` | `internal/httpapi/**` | `go test ./internal/httpapi/...` |
| S09 | Restart and two-instance integration tests | `book.persisted` | `internal/httpapi/**` or `internal/store/**` | `go test ./... (BOOKAPI_TEST_DSN set)` |
| S10 | Cheat corpus | all | `.factory/cheats/**` | `go test ./...` plus cheat review |
| S11 | Store `Delete` method | `book.delete_effective_and_idempotent` (enabling) | `internal/store/**` | `go test ./internal/store/... -run Delete (BOOKAPI_TEST_DSN set)` |
| S12 | HTTP `DELETE` route, handler, and `Store.Delete` interface method | `book.delete_effective_and_idempotent` (enabling; handler contract against a fake) | `internal/httpapi/**`, `README.md` | `go test ./internal/httpapi/... -run Delete` |
| S13 | End-to-end delete: PUT → DELETE → DELETE → GET 404; ISBN-10 form; concurrent deletes | `book.delete_effective_and_idempotent` (closing) | `internal/httpapi/**` (integration test using the real `store.Store`) | `go test ./internal/httpapi/... -run Delete (BOOKAPI_TEST_DSN set)` |
| S14 | Cheat corpus for deletion | `book.delete_effective_and_idempotent` (hardening) | `.factory/cheats/**` | `go test ./...` plus cheat review |

## Frames

- S01 must not import `internal/store` or `internal/httpapi` yet.
- S02 must not depend on anything outside the standard library.
- S04 must not define an interface; the HTTP layer owns its own dependency.
- S05 must not change `internal/store` or `internal/isbn`.
- Every slice must keep the existing suite green.
- S11 must not change `internal/httpapi/**` and must not return `ErrNotFound` for a missing row.
- S12 must not change `internal/store/**` or `internal/isbn/**` and must not return 404 for a valid absent ISBN.
- S13 may change only tests; it must not change `internal/store/**`, `internal/isbn/**`, or non-test `internal/httpapi/**`.
- S14 must not change production code.
- S11 and S12 share only the signature `Delete(ctx context.Context, isbn string) error`; they have disjoint file scopes and may be implemented in parallel. S13 depends on both.
- S11–S14 close one property across enabling and achieving slices; the coupling is deliberate and only S13 closes it.

## Definition of done

- `go test ./...` passes with no `BOOKAPI_TEST_DSN` (integration tests skipped).
- `go test ./...` passes with `BOOKAPI_TEST_DSN` set against a real Postgres.
- `go vet ./...` and `gofmt -l .` are clean.
- The ledger's `evidence` fields name the artifact that establishes each property.
| S15 | Remove internal packages and add the structural check | `code.no_internal_package` | all package directories, `cmd/api/structure_test.go` | `go test ./...` |
