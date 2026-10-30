# Evidence

Recorded 2026-10-29.

| Property | Class | Artifact | Assumption |
|---|---|---|---|
| `isbn.canonical` | tested | `internal/isbn/isbn_test.go` (30 cases); `FuzzCanonicalIdempotent` (2.1M execs) | none |
| `api.read_shape` | tested | `internal/httpapi/httpapi_test.go` (fake store) | none |
| `api.error_shape` | tested | `internal/httpapi/httpapi_test.go`; asserts no `pq:` or `SQLSTATE` in any body | none |
| `api.survives_db_outage` | tested | `internal/httpapi/httpapi_test.go`; blocking fake, 1s handler deadline | none |
| `api.body_limit` | tested | `internal/httpapi/httpapi_test.go`; 2 MiB body returns 413 | none |
| `ops.liveness_vs_readiness` | tested | `cmd/api/main_test.go`; ready error returns 503, healthz stays 200 | none |
| `book.persisted` | **assumed** | `internal/store/store_test.go` (skipped: `BOOKAPI_TEST_DSN` unset) | Postgres primary key enforced |
| `write.idempotent` | **assumed** | `internal/store/store_test.go` (skipped) | same |
| `write.no_duplicate_under_concurrency` | **assumed** | `internal/store/store_test.go` (skipped) | same |
| `book.delete_effective_and_idempotent` | **assumed** | `internal/httpapi/httpapi_test.go` (fake store); `internal/httpapi/delete_integration_test.go` and the store Delete tests (skipped) | Postgres delete-where-missing commits successfully |

## Not yet demonstrated

The three database properties are implemented and tested, but no Postgres or Docker was available, so the suite skipped them. They are `assumed`, not `tested`, until this passes against a real engine:

```sh
BOOKAPI_TEST_DSN='postgres://user:pass@localhost:5432/bookapi?sslmode=disable' \
  go test ./internal/store/... -count=1
```

## Judged

The Gallant gate over `internal/httpapi/httpapi.go`, `internal/isbn/isbn.go`, and `cmd/api/main.go` returned clean on 2026-10-29, with `GALLANT_MODEL=jev-1.13`. This is `judged` evidence and does not promote any property above `tested`.

## Delete property: cheat corpus

`go test ./internal/httpapi/...` rejects four of five cheats:

- `001-delete-noop` — rejected
- `003-skip-canonicalization` — rejected
- `004-body-on-204` — rejected
- `005-swallow-store-error` — rejected

`002-delete-absent-404` is **accepted**. The fake store returns `nil` by design, and the only tests that can see the defect require `BOOKAPI_TEST_DSN`. Until the integration suite runs against Postgres, this is a live regression invisible to `go test ./...`.

The gate initially flagged `internal/store/store.go` for internal error text. That was a false positive: the store writes no responses. The rule was amended to choose `client-safe` for files that do not write responses, and the gate then passed.

## Corner not covered

`api.body_limit` is tested at the handler. The server's `MaxHeaderBytes` and the proxy's body limit are not. If this service is placed behind a proxy that buffers bodies, the property must be re-tested at that boundary.
