# Slices

Outcome-level only. The implementer chooses files, packages, signatures, and
tests.

| # | Outcome | Properties |
|---|---|---|
| R1 | ISBN identity: canonical, validated, one form per book | `isbn.canonical` |
| R2 | Persistence: write, read, restart, second instance | `book.persisted` |
| R3 | Writes are idempotent and safe under concurrency | `write.idempotent`, `write.no_duplicate_under_concurrency` |
| R4 | HTTP read and write contract | `api.read_shape`, `api.body_limit` |
| R5 | Delete is effective and idempotent | `book.delete_effective_and_idempotent` |
| R6 | Failure surface and operations | `api.error_shape`, `api.survives_db_outage`, `ops.liveness_vs_readiness` |
| R7 | No internal directory | `code.no_internal_package` |

## Definition of done

- `go test ./...` passes.
- Each property's required evidence is recorded in `.factory/evidence.md`.
- `go vet ./...` and `gofmt -l` are clean.
