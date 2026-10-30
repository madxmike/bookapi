# External contract

This is the observable surface the implementation must provide. It does not
prescribe packages, files, functions, or algorithms.

## Service

- HTTP with JSON bodies.
- Configuration: `BOOKAPI_DSN` required; `BOOKAPI_ADDR` defaults to `:8080`.
- Persistence: PostgreSQL.
- Multiple instances may run against one database.

## Book fields

`isbn` (string, canonical ISBN-13), `title` (string), `author` (string),
`published_year` (integer).

## Path parameter

`{isbn}` accepts ISBN-10 or ISBN-13. Hyphens and spaces are ignored. A trailing
`x` is accepted for ISBN-10. Input is case-insensitive for the check character.

## PUT /books/{isbn}

Body: `{"title": string, "author": string, "published_year": integer}`.

- 200 with the stored record.
- 400 `invalid_isbn` for a wrong length, non-digit characters, an invalid check
  digit, or a prefix other than 978/979.
- 400 `invalid_body` for malformed JSON, unknown fields, or an empty title/author.
- 413 `payload_too_large` above 1 MiB.
- 503 `unavailable` when storage is unreachable.
- Repeating the same request yields the same record and one row.
- Last write wins.

## GET /books/{isbn}

- 200 with the book fields.
- 404 `not_found` when absent.
- 400 `invalid_isbn` on invalid input.
- 503 `unavailable` when storage is unreachable.

## DELETE /books/{isbn}

- 204 with an empty body, whether or not the row existed.
- 400 `invalid_isbn` on invalid input.
- 503 `unavailable` when storage is unreachable.
- After success, GET returns 404.
- Request bodies are ignored.

## GET /books

- 200 with a JSON array of book objects.
- Ordered by `isbn` ascending.
- An optional `author` query parameter filters to an exact match.
- No matches returns `200 []`.
- 503 `unavailable` when storage is unreachable.

## HEAD /books/{isbn}

- Identical status and headers to `GET /books/{isbn}`, including `Content-Length`.
- No body.

## Common response headers

Every response from every route, including health, readiness, the route-not-found
handler, and every error, carries:

- `X-Content-Type-Options: nosniff`
- `Cache-Control: no-store`

The headers are applied at one boundary. No handler may write a response outside
it.

## Identity

- The canonical form is ISBN-13.
- ISBN-10 converts to its 978-prefixed ISBN-13.
- 979-prefixed ISBN-13 values remain unchanged.
- The ISBN-10 and ISBN-13 forms of one book address the same record.

## Errors

Body: `{"error":{"code":"...","message":"..."}}`.

Codes: `invalid_isbn`, `invalid_body`, `payload_too_large`, `not_found`,
`unavailable`. No driver text, SQL, stack traces, or file paths.

## Operations

- `GET /healthz`: 200 while the process runs; never consults storage.
- `GET /readyz`: 200 when storage is reachable, 503 otherwise.
- A storage outage returns 503 within 2 seconds; healthz stays 200.

## Structure

- No directory named `internal` at any depth.
