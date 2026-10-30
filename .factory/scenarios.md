# Scenarios

| # | When | Then | Within | Evidence | Property |
|---|---|---|---|---|---|
| 1 | `PUT /books/9780131103627` with a valid body | 200, canonical ISBN echoed | 500 ms | response, row | `book.persisted` |
| 2 | `GET /books/9780131103627` | 200, same fields | 500 ms | response | `api.read_shape` |
| 3 | `GET` a valid but absent ISBN | 404, code `not_found` | 500 ms | response | `api.read_shape` |
| 4 | `GET /books/978013110362` (bad checksum) | 400, code `invalid_isbn` | 500 ms | response | `api.read_shape` |
| 5 | `PUT` the same body twice | identical response, one row | 1 s | response, `COUNT(*)` | `write.idempotent` |
| 6 | `PUT` the same ISBN with a new title | 200, one row, title updated | 1 s | row | `write.idempotent` |
| 7 | 32 concurrent `PUT`s of one ISBN | all 2xx, one row | 5 s | `COUNT(*)` | `write.no_duplicate_under_concurrency` |
| 8 | Restart the process, then `GET` | 200, record present | 5 s | response | `book.persisted` |
| 9 | `PUT` on instance A, `GET` on instance B | 200 | 1 s | both responses | `book.persisted` |
| 10 | Stop the database | 503 within 2 s, no driver text; `/healthz` 200; `/readyz` 503 | 2 s | responses | `api.survives_db_outage`, `ops.liveness_vs_readiness` |
| 11 | `GET` the ISBN-10 form of a stored book | same record as the ISBN-13 form | 500 ms | response | `isbn.canonical` |
| 12 | A 2 MiB `PUT` request body | 413 | 1 s | response | `api.body_limit` |
| 13 | `DELETE /books/9780131103627` after storing it | 204, empty body; `COUNT(*)` = 0 | 500 ms | response, row | `book.delete_effective_and_idempotent` |
| 14 | `DELETE` the same ISBN a second time | 204, empty body; still `COUNT(*)` = 0 | 500 ms | response, row | `book.delete_effective_and_idempotent` |
| 15 | `DELETE` a valid but never-stored ISBN | 204, empty body; no row created | 500 ms | response, row | `book.delete_effective_and_idempotent` |
| 16 | `DELETE` a stored ISBN, then `GET` it | GET 404, code `not_found` | 500 ms | both responses | `book.delete_effective_and_idempotent` |
| 17 | `DELETE /books/0131103628` (ISBN-10 of a stored ISBN-13) | 204; GET via both forms returns 404 | 500 ms | response, row | `book.delete_effective_and_idempotent`, `isbn.canonical` |
| 18 | `DELETE /books/978013110362` (bad checksum) | 400, code `invalid_isbn`; row still present | 500 ms | response, row | `book.delete_effective_and_idempotent`, `isbn.canonical` |
| 19 | 16 concurrent `DELETE`s of one stored ISBN | all 204; `COUNT(*)` = 0; no 5xx | 5 s | responses, `COUNT(*)` | `book.delete_effective_and_idempotent` |
| 20 | Stop the database, then `DELETE` a stored ISBN | 503, code `unavailable`, no driver text | 2 s | response | `book.delete_effective_and_idempotent`, `api.survives_db_outage`, `api.error_shape` |

## Must never happen

- Two rows for one book (`write.no_duplicate_under_concurrency`).
- A response body containing `pq:`, `SQLSTATE`, a stack trace, or a file path (`api.error_shape`).
- `/healthz` failing because the database is down (`ops.liveness_vs_readiness`).
- A 2xx for an invalid check digit (`isbn.canonical`).
- A 404 from a DELETE of a valid but absent ISBN (`book.delete_effective_and_idempotent`; would break idempotence).
- A row still present after a 204 DELETE (`book.delete_effective_and_idempotent`).
- A 204 carrying a non-empty body or a JSON body (`book.delete_effective_and_idempotent`).
- An invalid-ISBN DELETE reaching storage, changing a row, or returning 204 instead of 400 (`book.delete_effective_and_idempotent`).
- A DELETE during a database outage returning 500, hanging past 2 s, or containing driver text (`api.survives_db_outage`, `api.error_shape`).
- A directory named `internal` exists in the module (`code.no_internal_package`).
