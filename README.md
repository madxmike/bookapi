# bookapi

A small HTTP API for a book catalog backed by Postgres. Books are keyed by
canonical ISBN-13; ISBN-10 input is normalized on the way in.

## Configuration

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `BOOKAPI_DSN` | yes | — | Postgres connection string. The server applies its migrations at startup. |
| `BOOKAPI_ADDR` | no | `:8080` | Address the HTTP server listens on. |

Example DSN:

```sh
export BOOKAPI_DSN='postgres://bookapi:bookapi@localhost:5432/bookapi?sslmode=disable'
```

## Run

```sh
make run
```

Or directly:

```sh
go run ./cmd/api
```

## Endpoints

| Method | Path | Success | Errors |
|---|---|---|---|
| `GET` | `/books` | 200 with a JSON array ordered by ISBN | 503 `unavailable` |
| `GET` | `/books/{isbn}` | 200 with the record | 400 `invalid_isbn`, 404 `not_found`, 503 `unavailable` |
| `HEAD` | `/books/{isbn}` | the same status and headers as `GET`, no body | the same as `GET` |
| `PUT` | `/books/{isbn}` | 200 with the stored record | 400 `invalid_isbn`, 400 `invalid_body`, 413 `payload_too_large`, 503 `unavailable` |
| `DELETE` | `/books/{isbn}` | 204 with an empty body | 400 `invalid_isbn`, 503 `unavailable` |
| `GET` | `/healthz` | 200 while the process runs | — |
| `GET` | `/readyz` | 200 when the database answers | 503 when it does not |

`GET /books` accepts an optional `author` query parameter. It filters to an
exact author match; no matches returns `200 []`.

Every response, including health, readiness, the route-not-found response, and
every error, carries `X-Content-Type-Options: nosniff` and
`Cache-Control: no-store`. The headers and the error envelope are applied at
one boundary (`httpapi.response.flush`), not by individual handlers. All routes
come from one table in `httpapi.routes`; registration happens only in the
table-driven loop in `httpapi.buildMux`.

`PUT` accepts a JSON body of at most 1 MiB:

```json
{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}
```

Success responses are JSON objects:

```json
{"isbn":"9780131103627","title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}
```

Error responses are JSON objects with a stable code:

```json
{"error":{"code":"not_found","message":"no book with that isbn"}}
```

Codes are `invalid_isbn`, `invalid_body`, `payload_too_large`, `not_found`, and
`unavailable`. Error messages never contain driver, SQL, or filesystem detail.

`/healthz` is liveness only: it stays 200 during a database outage so the
process is not restarted. `/readyz` pings the database with a 1 second timeout
and returns 503 when that fails.

## Examples

Write a book (either ISBN form works; the response is always the ISBN-13):

```sh
curl -i -X PUT http://localhost:8080/books/0131103628 \
  -H 'Content-Type: application/json' \
  -d '{"title":"The C Programming Language","author":"Kernighan and Ritchie","published_year":1988}'
```

Read it back:

```sh
curl -i http://localhost:8080/books/9780131103627
```

A missing book:

```sh
curl -i http://localhost:8080/books/9780306406157
# HTTP/1.1 404 Not Found
# {"error":{"code":"not_found","message":"no book with that isbn"}}
```

A bad check digit:

```sh
curl -i http://localhost:8080/books/978013110362
# HTTP/1.1 400 Bad Request
# {"error":{"code":"invalid_isbn","message":"isbn is not valid"}}
```

Delete a book (either ISBN form works; a repeat delete still returns 204):

```sh
curl -i -X DELETE http://localhost:8080/books/9780131103627
# HTTP/1.1 204 No Content
```

Liveness and readiness:

```sh
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

## Test

```sh
make test
```

Or directly:

```sh
go test ./...
```

The handler tests use an in-memory store and need no database. The store and
HTTP integration tests run only when `BOOKAPI_TEST_DSN` is set:

```sh
BOOKAPI_TEST_DSN="$BOOKAPI_DSN" go test ./...
```
