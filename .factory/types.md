# Type design

## Scope and method

This document maps each property in `.factory/ledger.yaml` onto Go's type
system. For every property it asks:

1. What is the invariant?
2. Can a value that violates it be constructed from outside the owning package?
3. If not, which type and constructor make it unrepresentable?
4. If yes, why (external system, time, concurrency, intent, or Go's type
   system), and what check covers it?

"Outside the package" is literal. For `isbn.ISBN` it means outside `isbn`; for
`book.Book` it means outside `book`; for `problem.Failure` it means outside
`problem`. This matters because HTTP handlers live in `httpapi` next to the
response boundary, so types declared in `httpapi` are **not** sealed from the
handlers.

Go limits assumed throughout, and not worked around:

- every struct has a zero value, and an opaque struct cannot exclude it;
- no sum types and no exhaustiveness checking;
- no refinement types (no "string of length 13", no "at most 1 MiB");
- no linear, effect, ownership, or session types;
- unexported fields seal a type from other packages only, never from its own;
- interfaces are structural; any type may satisfy an exported interface;
- `unsafe` and reflection can violate any of the above.

The goal is not to call everything "type-safe". It is to move what can move,
and to name precisely what cannot.

## Summary

| Class | Properties |
|---|---|
| Value-encoded modulo the zero value | `isbn.canonical`, `api.list_books` (if `book.List` is adopted) |
| Partially encoded | `api.read_shape`, `api.error_shape`, `book.delete_effective_and_idempotent`, `api.head_book`, `api.handler_uniform` |
| Not encodable | `book.persisted`, `write.idempotent`, `write.no_duplicate_under_concurrency`, `api.survives_db_outage`, `ops.liveness_vs_readiness`, `api.body_limit`, `code.no_internal_package`, `api.handler_registration` |

No property is fully enforced. Even `isbn.canonical` leaves `isbn.ISBN{}`
representable. The escape list at the end is a required part of this design.

## Invariant table

The "violating value constructible outside?" column asks about a value, not an
operation. Operational properties are marked "yes" even when a related key type
is encoded.

| Property | Invariant | Violating value constructible outside? | Type or constructor | What the compiler prevents | Residual |
|---|---|---|---|---|---|
| `isbn.canonical` | Every ISBN is a canonical, checksum-valid ISBN-13; ISBN-10 maps to its 978 form; 977 and bad check digits are rejected. | Yes, but only `isbn.ISBN{}`; no other violating value is constructible. | `isbn.ISBN` with unexported `digits string`; `isbn.Parse(string) (ISBN, error)`; `ISBN.String()`, `ISBN.Valid()`. | A raw `string` cannot be passed where `isbn.ISBN` is required; no package outside `isbn` can construct a non-zero invalid ISBN or mutate one after construction. | The zero value is invalid and constructible; `unsafe`/reflection; code that still carries `string` after migration. Check: `TestCanonical`, `FuzzCanonicalIdempotent`, `TestPutStoresCanonicalISBN`, `TestGetViaISBN10`, `TestDeleteIntegrationISBN10`, cheat `001-no-prefix-guard`. |
| `book.persisted` | A PUT is durable across a restart and visible to a second instance. | Yes. Durability is not a value. | `store.Open` returns the only Postgres-backed `*store.Store`; `httpapi.New` accepts a `Store` interface. | Nothing about durability; the compiler only ensures the handler calls some `Store`. | A fake or in-memory `Store` satisfies the interface; production wiring is not typed. Check: `TestProcessRestartPersistsBook`, `TestPutGetAcrossInstances`, `TestPutGetAcrossRestart`, `TestCloseAndReopenStillReads`, `TestTwoStoresSeeEachOthersWrites`. |
| `write.idempotent` | Repeating a PUT with the same ISBN yields the same record and one row. | Yes. It is an operation and effect property. | `isbn.ISBN` makes both spellings the same key; `store.Put(ctx, book.Book) error` is the write surface. | Prevents two spellings of one ISBN from becoming different keys and prevents a raw string key; the zero `Book` is still constructible, so `Put` must reject `!b.Valid()` at runtime. | `Put` can be a plain `INSERT`; `ON CONFLICT` can be omitted; the handler can send a different body. Check: `TestPutIdenticalTwiceKeepsOneRow`, `TestPutTwiceLastWriteWins`, `TestPutIsIdempotentAndLastWriteWins`, cheat `001-upsert-does-nothing`. |
| `write.no_duplicate_under_concurrency` | Concurrent PUTs of one ISBN produce at most one row. | Yes. Concurrency and storage. | `isbn.ISBN` gives one key; the schema's `isbn text PRIMARY KEY` is the uniqueness mechanism. | Prevents different forms of one ISBN from becoming different keys. | The primary key can be dropped; the upsert can be replaced by a read-then-write race; a client-side check can race. Check: `TestConcurrentPutsKeepOneRow` (32 writers), `TestConcurrentPutsOneRow` (32 HTTP writers), race suite. |
| `api.read_shape` | GET returns 200 with stored fields, 404 `not_found` when absent, and 400 `invalid_isbn` on bad input. | Partly. The success shape and error vocabulary are encodable; the mapping from store outcome to status is intent. | `httpapi.bookResponse` and `response.OK`; `isbn.Parse` before every store call; `problem.BookNotFound` and `problem.InvalidISBN`. | `OK` accepts only app-owned success shapes and emits 200; handlers cannot compose an error code; `Get` cannot be called without an `isbn.ISBN`. | A handler can map `store.ErrNotFound` to 200 or 503, or map an outage to 404; the compiler does not inspect the branch. Check: `TestHandlers`, `TestHeadMirrorsEveryGetStatus`, `TestGetMissingReturnsErrNotFound`, integration tests. |
| `api.error_shape` | No error body contains driver text, SQL, a stack trace, or a file path; every error uses the envelope. | Mostly. Across packages yes; within `httpapi` only if `failure` moves to `problem` and no raw writer is added. | `problem.Failure` (unexported fields, named values, `Status()`, `Body()`); `response.Fail(problem.Failure)`; `ServeHTTP` recover; `wire`/`adapt` capability. | Outside `problem`, no code can choose a status, code, or message, or put `err.Error()` in the body; handlers never receive an `http.ResponseWriter`. | Same-package code can add a raw write method or a second boundary; `problem.Failure{}` is constructible but rejected at runtime; logs can still leak (the property is response-only). Check: `assertErrorEnvelope`, `TestEveryErrorClassIsUniform`, `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503`, cheat `001-stack-trace-in-503`, `outage.log`. |
| `api.survives_db_outage` | When the database is unreachable, requests return 503 within 2s and `/healthz` stays 200. | Yes. External availability and time. | `context.Context` deadlines derived in `httpapi` (`storeTimeout`); `problem.Unavailable`. | Nothing about the 2s bound or about a handler remembering a deadline. | A handler can call the store without a timeout; a store can ignore `ctx`; healthz is a separate route. Check: `TestStoreTimeoutReturns503`, `TestListTimeoutReturns503`, `TestLivenessAndReadiness`, `outage.log`. |
| `ops.liveness_vs_readiness` | `/healthz` never consults storage; `/readyz` fails when storage is unreachable. | Yes. A healthz that reads storage can be written; intent and wiring. A partial capability move is possible. | `liveness` with no fields hosts `healthz`; `readiness{probe Probe}` hosts `readyz`; `New(st, probe)` requires a probe. | `healthz` has no receiver state, so it cannot reach the server's store or probe. | The probe can be a no-op; `healthz` can still import `store` and open its own connection; `New` can receive a nil probe unless it rejects nil. Check: `TestLivenessAndReadiness`, `TestHealthzNeverConsultsStorage`, `outage.log`. |
| `api.body_limit` | A request body larger than 1 MiB is rejected with 413. | Yes. Runtime input size; Go has no refinement types. | `problem.PayloadTooLarge`; `decodeInput` with `io.LimitReader(maxBodyBytes+1)`. | Nothing about the number; the compiler cannot express "at most 1 MiB". | A handler can bypass `decodeInput`; the limit constant can be wrong; `Content-Length` is not trusted. Check: `TestHandlers/put_oversized_body`, `/put_oversized_trailing_body`, `TestBodyLimitIntegration` (2 MiB). |
| `book.delete_effective_and_idempotent` | DELETE of a valid ISBN returns 204 with no body and removes the row; a repeat returns 204; a later GET returns 404; an invalid ISBN returns 400 without touching storage. | Partly. The key type and the 204 shape are encodable; the effect and idempotence are not. | `isbn.ISBN`; `response.NoContent()` as the only 204 path; `store.Delete(ctx, isbn.ISBN) error` with no not-found sentinel in the signature. | A raw string cannot reach `Delete`; 204 cannot carry a body; a handler cannot choose 400 versus 204 through `NoContent`. The zero ISBN is still constructible, so the store should reject `!id.Valid()`. | `Delete` can be a no-op or return `ErrNotFound`; the handler can map an error to 404 or 503; the database can fail. Check: `TestDelete`, `TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationAbsent`, `TestDeleteIntegrationInvalidLeavesRow`, `TestDeleteIntegrationConcurrent`, `TestConcurrentDeletesRemoveRow`, six delete cheats. |
| `code.no_internal_package` | No directory named `internal` at any depth. | Yes. Filesystem layout, not a value. | None. | Nothing. | Any directory can be created; only a filesystem walk sees it. Check: `TestNoInternalPackage`, cheat results. |
| `api.list_books` | GET /books returns 200 with a JSON array ordered by ISBN; an optional author query filters exact matches; no matches returns `[]`. | No for ordering and non-nil once `book.List` is adopted; yes for the SQL filter. The zero list is empty and valid. | `book.List` with `NewList([]Book) (List, error)` validating ascending ISBN, no duplicates, and copying to a non-nil slice; `All()` returns a copy; `httpapi.bookList`. | A non-empty out-of-order or duplicate `book.List` cannot be constructed outside `book`; the response is an app-owned slice, not `any`. | The store can omit `ORDER BY` (then `NewList` errors); the filter can use `LIKE`, case folding, or trimming; `book.List{}` is empty and valid. Check: `TestListOrderedAndFiltered`, `TestListEmptyAuthorIsAnExactFilter`, `TestListNoMatchesIsEmptyNotNil`, `TestListIntegrationOrdered`, `TestListIntegrationAuthorFilter`. |
| `api.head_book` | HEAD has the same status and headers as GET, including `Content-Length`, and no body. | Partly. Same handler and boundary suppression are encodable; a separate HEAD handler is still possible. | `route{methods []string, pattern string, handler handler}` with GET and HEAD on one entry sharing `s.get`; `response.head` set from `r.Method`; `flush` suppresses the body and keeps `Content-Length`. | Within one route entry both methods use the same handler value; handlers have no raw writer, so they cannot emit a body for HEAD. | A separate HEAD route can be added; a handler can branch on `r.Method`; suppression depends on `response.head`. Check: `TestHeadMirrorsGet`, `TestHeadMirrorsEveryGetStatus`, `TestHeadIntegrationMirrorsGet`, `TestRealServerHeadAndList`. |
| `api.handler_uniform` | Every response from every dispatch path carries `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`; every error uses the stable envelope. | Mostly. Strong across packages; same-package structure is a check. | Unexported `server`, `mux`, `route`, `handler`, `response`, `wire`; `New` returns `http.Handler`; `adapt` capability check; one `ServeHTTP`; `flush` applies the common headers. | External packages cannot name `handler`, reach `mux`, register routes, or give a handler a raw writer; handlers cannot write outside `response`. | Same-package code can add a second dispatch path or raw write; a caller can wrap the returned handler, which is outside this property. Check: `TestEveryRouteIsUniform`, `TestEveryErrorClassIsUniform`, `TestRouteTableIsTheOnlyRegistration`, `TestDiscoveryRejectsSecondDispatchPath`. |
| `api.handler_registration` | No handler is reachable outside the route table, by any signature, alias, method value, secondary dispatch path, or dynamic construction. | Yes. Any function of the shape is a handler; Go has no nominal handler kind. | Unexported `handler`; one `routes()` table; `buildMux` as the only registration; `adapt` as the only adapter. | External packages cannot name or construct a `handler`; route registration is not exported. | Within `httpapi`, any method with the shape is a handler; aliases, method values, literals, reflection, and a second registration can exist. Check: `TestRouteTableIsTheOnlyRegistration` and the eleven `TestDiscoveryRejects*` cases. |

## Type sketches

These are designs, not implementations. Names, visibility, and signatures are
the part that matters; bodies are omitted.

### `isbn`

```go
package isbn

// ISBN is a canonical, checksum-valid ISBN-13.
// The zero value is invalid.
type ISBN struct {
	digits string // exactly 13 ASCII digits; prefix 978 or 979; valid check
}

var ErrInvalid = errors.New("invalid isbn")

// Parse ignores hyphens and spaces, accepts X/x as an ISBN-10 check digit,
// and returns the canonical ISBN-13 or ErrInvalid.
func Parse(s string) (ISBN, error)

func (i ISBN) String() string // "" for the zero value
func (i ISBN) Valid() bool    // false for the zero value
```

- `Parse` is the only constructor. It carries the normalization and the
  validation, so `isbn.canonical` is a property of the value, not of a call
  site that remembered to normalize.
- `ISBN` is comparable, so it is usable as a map key and with `==`.
- Do not add `UnmarshalText` or a second exported constructor unless a second
  entry point is genuinely needed. A second constructor is a second place to
  forget the prefix and check-digit rules.
- Remove `Canonical`. Keeping it as a string-returning wrapper preserves the
  exact escape the type is meant to remove.

### `book`

```go
package book

type Book struct {
	isbn   isbn.ISBN
	title  string
	author string
	year   int
}

var ErrInvalid = errors.New("invalid book")

// New rejects an invalid ISBN and a title or author that is empty after
// trimming whitespace. It stores the original strings, not trimmed copies.
func New(id isbn.ISBN, title, author string, year int) (Book, error)

func (b Book) ISBN() isbn.ISBN
func (b Book) Title() string
func (b Book) Author() string
func (b Book) PublishedYear() int
func (b Book) Valid() bool // false for the zero value

// List is an ordered, duplicate-free catalog slice.
// The zero value is the empty list.
type List struct {
	books []Book
}

// NewList validates that every Book is valid, that ISBNs are strictly
// ascending, and that there are no duplicates. It copies the input and never
// stores a nil slice.
func NewList(bs []Book) (List, error)

func (l List) Len() int
func (l List) All() []Book // returns a copy; empty is non-nil
```

- `Book` is opaque. An external package cannot create a `Book` with a raw
  string ISBN, an empty title, or a zero ISBN through `New`, and cannot mutate
  one after construction.
- `List` makes ordering a property of the value. `NewList` does not sort; it
  rejects. That keeps D18 intact: storage owns the order, and a missing
  `ORDER BY` fails loudly instead of returning an unordered 200.
- `All` returns a copy. Returning the internal slice would let a caller break
  the ordering invariant after construction.
- The zero `List` is empty and valid. The zero `Book` is invalid. Both facts
  must be stated; Go cannot remove either.

### `store`

```go
package store

var ErrNotFound = errors.New("book not found")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error)

func (s *Store) Put(ctx context.Context, b book.Book) error
func (s *Store) Get(ctx context.Context, id isbn.ISBN) (book.Book, error)
func (s *Store) Delete(ctx context.Context, id isbn.ISBN) error
func (s *Store) List(ctx context.Context, author *string) (book.List, error)
func (s *Store) Count(ctx context.Context, id isbn.ISBN) (int, error)
func (s *Store) Ping(ctx context.Context) error
func (s *Store) Migrate(ctx context.Context) error
```

- The signature change from `isbn string` to `isbn.ISBN` is the important
  one: after migration, a non-canonical string cannot reach SQL from inside
  the module without an explicit `isbn.Parse` whose error was ignored.
- Scanning a row constructs the domain value rather than assigning strings:
  `isbn.Parse(row.ISBN)` then `book.New(id, title, author, year)`. A corrupt
  row becomes an error, not a `Book` with a bad ISBN.
- `List` returns `book.List`, so the ordering and non-nil invariants are
  validated at the storage boundary.
- Keep `ErrNotFound` for `Get`. Do **not** introduce a not-found return for
  `Delete`; absence is success, and the type signature should not suggest
  otherwise. A defensive `errors.Is(err, ErrNotFound)` in the handler is still
  acceptable for stores that report absence, but it is not the contract.
- `Put`, `Get`, `Delete`, and `Count` should still check `b.Valid()` or
  `id.Valid()` at the boundary. The zero value is constructible from outside
  and would otherwise insert an empty ISBN.

### `httpapi/problem`

A separate package is what makes the error envelope a compile-time boundary
for handler code. The package name is not `internal`; it is an ordinary
subpackage.

```go
package problem

// Failure is a declared error response. Fields are unexported and there is
// deliberately no exported constructor.
type Failure struct {
	status  int
	code    string
	message string
}

func (f Failure) Status() int
func (f Failure) Code() string
func (f Failure) Message() string
func (f Failure) Body() []byte // the {"error":{"code","message"}} envelope

var (
	InvalidISBN      = Failure{400, "invalid_isbn", "isbn is not valid"}
	InvalidBody      = Failure{400, "invalid_body", "request body must be a JSON object with title, author, and published_year"}
	EmptyFields      = Failure{400, "invalid_body", "title and author must not be empty"}
	PayloadTooLarge  = Failure{413, "payload_too_large", "request body exceeds 1 MiB"}
	BookNotFound     = Failure{404, "not_found", "no book with that isbn"}
	RouteNotFound    = Failure{404, "not_found", "no such route"}
	Unavailable      = Failure{503, "unavailable", "the book store is unavailable"}
	Internal         = Failure{500, "internal", "internal error"}
)
```

- Outside `problem`, a handler cannot write
  `problem.Failure{status: 500, code: "x", message: err.Error()}`. It can only
  name one of the declared values. `Body()` owns the envelope, so `httpapi`
  cannot add fields or interpolate driver text.
- The zero `problem.Failure{}` is constructible (`problem.Failure{}` is legal
  even with unexported fields), but `response.Fail` rejects `Status() < 400`
  at runtime and the boundary turns the resulting panic into `Internal`.
- Keep the status, code, and message text exactly as they are today. The
  `internal` code is not in the contract's enumerated code list; see the
  fall-out section. That is a contract question, not a reason to widen the
  type.

### `httpapi`

```go
package httpapi

type Store interface {
	Get(ctx context.Context, id isbn.ISBN) (book.Book, error)
	Put(ctx context.Context, b book.Book) error
	Delete(ctx context.Context, id isbn.ISBN) error
	List(ctx context.Context, author *string) (book.List, error)
}

type Probe func(context.Context) error

// New returns the single top-level handler. A nil store or nil probe is a
// wiring error; prefer an error return over silently substituting a no-op.
func New(st Store, probe Probe) (http.Handler, error)

type handler func(*response, *http.Request)

type route struct {
	methods []string // empty means any method (catch-all)
	pattern string
	handler handler
}

func (rt route) paths() []string

type server struct {
	store Store
	probe Probe
	mux   *http.ServeMux
}

// liveness has no fields, so healthz cannot reach the store or the probe.
type liveness struct{}

func (liveness) healthz(w *response, r *http.Request)

type readiness struct {
	probe Probe
}

func (r readiness) readyz(w *response, req *http.Request)

type success interface {
	isSuccess()
}

type bookResponse struct {
	ISBN          string `json:"isbn"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	PublishedYear int    `json:"published_year"`
}

func (bookResponse) isSuccess()

type bookList []bookResponse

func (bookList) isSuccess()

func (w *response) OK(body success)   // 200 only
func (w *response) Text(body string)  // 200 only
func (w *response) NoContent()        // 204, no body, no Content-Type
func (w *response) Fail(f problem.Failure)
```

- `New` returns `http.Handler`, not `*server`. Callers in other packages cannot
  reach `mux`, `routes`, `handler`, `response`, or `wire`.
- `New` takes a required `Probe` instead of a variadic option and a `Ping`
  type assertion. Readiness can no longer be omitted by accident, though a
  non-nil no-op function is still possible.
- `response.OK` has no status argument. A success body cannot be sent as 201,
  204, or 500 by mistake, and `bookResponse`/`bookList` are the only shapes it
  accepts. `NoContent` is the only 204 path, so a 204 with a body is not
  expressible through the API.
- `response.Text` has no status argument; health and readiness are always 200,
  and failures go through `Fail`.
- `route.methods` lets GET and HEAD share one handler value in one table entry.
  The route table becomes:

```go
return []route{
	{methods: []string{http.MethodGet}, pattern: "/healthz", handler: liveness{}.healthz},
	{methods: []string{http.MethodGet}, pattern: "/readyz", handler: readiness{probe: s.probe}.readyz},
	{methods: []string{http.MethodGet}, pattern: "/books", handler: s.list},
	{methods: []string{http.MethodPut}, pattern: "/books/{isbn}", handler: s.put},
	{methods: []string{http.MethodGet, http.MethodHead}, pattern: "/books/{isbn}", handler: s.get},
	{methods: []string{http.MethodDelete}, pattern: "/books/{isbn}", handler: s.delete},
	{methods: nil, pattern: "/", handler: s.notFound},
}
```

- `buildMux` loops over `rt.paths()` and is still the only registration site.
  `adapt` still refuses a writer that is not the boundary's `*wire`, so a
  handler cannot be run with a raw `http.ResponseWriter`.
- `ServeHTTP` remains the single dispatch path. The deferred recover calls
  `res.Fail(problem.Internal)`; panic text is never written to the response.

### Visibility summary

| Type | Package | Exported | Fields | Constructor |
|---|---|---|---|---|
| `ISBN` | `isbn` | yes | unexported | `Parse` |
| `Book` | `book` | yes | unexported | `New` |
| `List` | `book` | yes | unexported | `NewList` |
| `Store` | `store` | yes | unexported | `Open` |
| `Failure` | `httpapi/problem` | yes | unexported | none; named values only |
| `Store` (interface) | `httpapi` | yes | n/a | implemented by `store.Store` and test fakes |
| `Probe` | `httpapi` | yes | n/a | caller supplies a function |
| `handler`, `route`, `server`, `response`, `wire` | `httpapi` | no | unexported | `New`, `buildMux`, `adapt` |
| `liveness`, `readiness` | `httpapi` | no | unexported | route table only |
| `bookResponse`, `bookList`, `success` | `httpapi` | no | mixed | handlers only |

## Escape list

Every entry below is a way a violating value or outcome can still be produced
given the proposed types. This list is the part of the design that keeps the
type sketches honest.

| Property | Why it is not encodable | What still escapes | Check that covers it |
|---|---|---|---|
| `isbn.canonical` | Go's type system: every struct has a zero value. | `isbn.ISBN{}` is constructible and invalid; `unsafe` or reflection can write `digits`; code can ignore a `Parse` error. | `TestCanonical`, `FuzzCanonicalIdempotent`, HTTP ISBN-10/977 tests, cheat `001-no-prefix-guard`; add `isbn.ISBN{}.Valid() == false`. |
| `book.persisted` | External system (Postgres), time (restart), distribution (second instance). | A fake or in-memory `Store` satisfies `httpapi.Store`; production can be wired to the wrong implementation; a write can be acknowledged before commit. | `TestProcessRestartPersistsBook`, `TestPutGetAcrossInstances`, `TestPutGetAcrossRestart`, `TestCloseAndReopenStillReads`. |
| `write.idempotent` | External system and intent: idempotence is a property of the SQL statement and the sequence of calls, not of a value. | `Put` can be a plain `INSERT`; `ON CONFLICT` can be dropped; a handler can send a different body for the same key (that is last-write-wins, but a bug can make it non-idempotent). | `TestPutIdenticalTwiceKeepsOneRow`, `TestPutTwiceLastWriteWins`, `TestPutIsIdempotentAndLastWriteWins`, cheat `001-upsert-does-nothing`. |
| `write.no_duplicate_under_concurrency` | Concurrency and external system. | The primary key can be removed; the upsert can be replaced by a read-then-write; a client-side uniqueness check can race. | `TestConcurrentPutsKeepOneRow`, `TestConcurrentPutsOneRow`, race suite; schema review. |
| `api.read_shape` | Intent: the mapping from a store result to a status is a branch, and Go does not check that the branch is exhaustive or correct. | A handler can return 200 for `ErrNotFound`, 503 for a missing row, or 404 for an outage. | `TestHandlers`, `TestHeadMirrorsEveryGetStatus`, `TestGetMissingReturnsErrNotFound`, integration tests. |
| `api.error_shape` | Go's type system: unexported fields do not seal a type from its own package, and handlers live in `httpapi`. | If `problem` is not adopted, same-package code can construct a `failure` with `err.Error()`. Even with `problem`, `httpapi` can add a raw `send` call or a second boundary. Logs can contain driver text (the property is response-only). | `assertErrorEnvelope`, `TestEveryErrorClassIsUniform`, `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503`, cheat `001-stack-trace-in-503`, `outage.log`; add a structural check for raw writes if `problem` is not adopted. |
| `api.survives_db_outage` | External system and time. | A handler can use `r.Context()` with no deadline; a store can ignore `ctx`; the 2s bound is a test observation, not a type. | `TestStoreTimeoutReturns503`, `TestListTimeoutReturns503`, `TestLivenessAndReadiness`, `outage.log`. |
| `ops.liveness_vs_readiness` | Intent and wiring. | The probe can always return nil; `healthz` can import `store` and open its own connection; `New` can be given a non-nil no-op. | `TestLivenessAndReadiness`, `TestHealthzNeverConsultsStorage`, `outage.log`. |
| `api.body_limit` | Go's type system: no refinement types. The size is runtime data. | A handler can read `r.Body` directly; `decodeInput` can be bypassed; the limit constant can be changed. | `TestHandlers/put_oversized_body`, `/put_oversized_trailing_body`, `TestBodyLimitIntegration`. |
| `book.delete_effective_and_idempotent` | External system and intent: effect and idempotence are operations, not values. | `Delete` can be a no-op or return `ErrNotFound`; the handler can map an error to 404 or 503; the database can fail after a 204 is decided. | `TestDelete`, `TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationAbsent`, `TestDeleteIntegrationInvalidLeavesRow`, `TestDeleteIntegrationConcurrent`, `TestConcurrentDeletesRemoveRow`, six delete cheats. |
| `code.no_internal_package` | Go's type system and module layout: no type describes a directory name. | Any directory named `internal` can be created at any depth; a symlink can be added. | `TestNoInternalPackage`, cheat results. |
| `api.list_books` | Intent (the SQL filter and ordering) and Go's zero value. | The store can omit `ORDER BY` (then `NewList` returns an error, so the endpoint fails loudly rather than returning an unordered 200); the filter can be `LIKE`, case-insensitive, or trimmed; `book.List{}` is empty and valid. | `TestListOrderedAndFiltered`, `TestListEmptyAuthorIsAnExactFilter`, `TestListNoMatchesIsEmptyNotNil`, `TestListIntegrationOrdered`, `TestListIntegrationAuthorFilter`. |
| `api.head_book` | Intent (the route table) and Go's structural function types. | A separate HEAD route can be added; a handler can branch on `r.Method`; same-package code can construct a `response` with the wrong `head` flag. | `TestHeadMirrorsGet`, `TestHeadMirrorsEveryGetStatus`, `TestHeadIntegrationMirrorsGet`, `TestRealServerHeadAndList`. |
| `api.handler_uniform` | Go's type system: package-level sealing plus a structural check, not a proof. | Same-package code can add a second dispatch path or a raw write; a caller can wrap the returned handler. | `TestEveryRouteIsUniform`, `TestEveryErrorClassIsUniform`, `TestRouteTableIsTheOnlyRegistration`, `TestDiscoveryRejectsSecondDispatchPath`. |
| `api.handler_registration` | Go's type system: a handler is any function with the shape; there is no nominal handler kind. | A method with the shape can exist outside the table; aliases, method values, literals, reflection, and a second registration can exist. | `TestRouteTableIsTheOnlyRegistration` and the eleven `TestDiscoveryRejects*` cases. |

## Fall-out analysis

Given only the proposed types and the contract, an implementer can still get
the following wrong. Each item names the decision the compiler leaves open.

1. **Zero values are legal.** `isbn.ISBN{}`, `book.Book{}`, `book.List{}`,
   `problem.Failure{}`, a nil `Probe`, and a typed-nil `*bookResponse` all
   compile. `Valid`, `Fail`, and `New` reject some of them at runtime, but a
   caller can skip the check. This is the single largest residual and it is
   inherent in Go.

2. **Constructor errors can be ignored.** `id, _ := isbn.Parse(s)` produces
   the zero ISBN. `b, _ := book.New(...)` produces an invalid book. No type
   prevents discarding the error. Add a store-level `Valid` guard and a
   corrupt-row test.

3. **The store is an interface.** `httpapi.Store` can be satisfied by an
   in-memory map, a different database, or a fake that loses writes. The
   compiler cannot tell production wiring from test wiring. Keep the real
   restart and two-instance tests, and make `cmd/api` the only production
   wiring.

4. **SQL is invisible.** `Put` can be a plain `INSERT`; `Delete` can be a
   no-op; `List` can omit `ORDER BY`; the author filter can be `LIKE` or
   case-insensitive. `isbn.ISBN` fixes the key, not the statement.

5. **The primary key is a schema fact.** `isbn text PRIMARY KEY` is what
   prevents duplicates under concurrency. A migration can drop or weaken it.
   The Go type cannot see the schema. Keep the concurrent tests and review
   migrations.

6. **Status mapping is a branch.** Nothing forces `store.ErrNotFound` to map
   to `problem.BookNotFound` and every other error to `problem.Unavailable`.
   A handler can invert the mapping. The handler table tests are the check.

7. **The error code set is open at the edges.** `problem` is a closed set of
   values, but a future edit can add one; there is no exhaustiveness check
   that every code is tested. `problem.Internal` uses code `internal`, which
   is not in the contract's enumerated list (`invalid_isbn`, `invalid_body`,
   `payload_too_large`, `not_found`, `unavailable`). The ledger's
   `api.error_shape` requires the envelope, not that code set. This is a
   contract question for the owner, not something this document changes.

8. **Time is a value.** `context.WithTimeout` can be omitted; `storeTimeout`
   can be changed; a store can ignore the deadline. The 2s property is
   observed by tests and the outage log, not by the type.

9. **Readiness is a function.** `Probe` can be `func(context.Context) error {
   return nil }`. The `liveness` split removes the server's store from
   `healthz`'s reach, but `healthz` can still import `store` and open its own
   connection. The type raises the cost; it does not prove the behavior.

10. **The body limit is a constant.** `decodeInput` can be bypassed, and the
    `io.LimitReader` bound can be wrong. No type expresses 1 MiB.

11. **HEAD can diverge.** A second HEAD route can be added, or a handler can
    branch on `r.Method`. The multi-method route entry makes the intended
    shape natural, not mandatory. A structural check can require HEAD to
    appear only with GET on one entry with the same handler.

12. **Uniformity is structural.** The discovery check proves that the route
    table is the only registration and that `ServeHTTP` has one dispatch. It
    does not prove that no same-package code calls `send` directly, adds a
    second mux, or wraps a handler. If `problem` is adopted, add a structural
    check that `send` is called only from `response.go` and that no
    `problem.Failure{}` composite literal exists in `httpapi`.

13. **Registration cannot be type-sealed.** Go has no distinct handler kind.
    The discovery check catches named functions, methods, literals, aliases,
    method values, default-mux registration, and `reflect.MakeFunc` in the
    module. A handler constructed in an imported package, or reached through
    an interface method with a different name, can still exist.

14. **Success shape has an interface hole.** `OK(nil)` compiles and sends
    `null`; a typed-nil `*bookResponse` compiles and sends `null`. Add a
    runtime nil check in `OK` if that matters. The marker interface prevents
    arbitrary types, not nil.

15. **ISBN normalization is fixed only at `Parse`.** A handler that trims
    tabs, Unicode spaces, or other separators before calling `Parse` accepts
    input the type would reject. The type fixes the rule only if `Parse` is
    the only entry point.

16. **Store errors are still strings.** `store` wraps driver errors with
    `%w`. The response boundary seals the body, but logs and any future
    caller that serializes `err.Error()` can leak. The property is about the
    response; keep it that way.

17. **`book.List.All` must copy.** An implementer optimizing the copy can
    return the internal slice, and a caller can then reorder or insert. The
    method signature cannot enforce the copy; code review and a test can.

18. **`problem` values are mutable inside `problem`.** A future edit can
    change a message or a status. There is no compiler check that the
    declared values match the contract. The envelope tests pin the messages
    that matter.

## Migration notes

### Package-by-package

| Area | Change | Files |
|---|---|---|
| `isbn` | Add `ISBN`, `Parse`, `String`, `Valid`; remove `Canonical`; keep `ErrInvalid` and `checkDigit13`. | `isbn/isbn.go`, `isbn/isbn_test.go` |
| `book` | Make `Book` opaque; add `New`, accessors, `Valid`, `ErrInvalid`; add `List`, `NewList`, `Len`, `All`. | `book/book.go`; new `book/book_test.go` |
| `store` | Change `Get`, `Delete`, `Count` to `isbn.ISBN`; scan via `isbn.Parse` and `book.New`; `List` returns `book.List`; guard `Valid` at the boundary. | `store/store.go`, `store/store_test.go`, `store/store_list_test.go` |
| `problem` | New ordinary subpackage; move `failure`, `errorResponse`, `errorDetail` there as `Failure` with unexported fields, named values, `Status`, `Code`, `Message`, `Body`. | new `httpapi/problem/problem.go` |
| `httpapi` | Update `Store`; replace variadic `New` with `New(st, probe)`; replace `json`/`text` with `OK`/`Text`; `NoContent` only 204; `Fail(problem.Failure)`; parse to `isbn.ISBN`; construct via `book.New`; list via `book.List`; `route.methods`; `liveness`/`readiness`. | `httpapi/httpapi.go`, `httpapi/handlers.go`, `httpapi/response.go` |
| `cmd/api` | Pass `st.Ping` as `httpapi.Probe`; handle the `New` error if the constructor returns one. | `cmd/api/main.go`, `cmd/api/main_test.go` |
| Tests | Update fixtures to `book.New`/`book.NewList`; update `isbn` tests to `Parse(...).String()`; update list tests to `book.List`/`All`; update discovery fixture for `route.methods` and the handler receiver set. | `httpapi/*_test.go`, `store/*_test.go`, `cmd/api/*_test.go` |
| Structural checks | Update `TestRouteTableIsTheOnlyRegistration` for `route.methods` and for handlers hosted on `liveness`/`readiness`; add a raw-write/declared-failure check if `problem` is adopted. | `httpapi/discovery_test.go` |

### Order of work

1. Introduce `isbn.ISBN` and `book.Book`/`book.List`. This is the largest
   compile break and the source of the strongest invariants. Update `store`
   and `httpapi` together so the module compiles.
2. Move `failure` to `httpapi/problem` and rename `response` methods. Keep the
   statuses, codes, and messages byte-for-byte identical; the existing
   envelope tests should pass unchanged.
3. Replace variadic `New` with the mandatory `Probe`. Update `cmd/api` and the
   test call sites.
4. Split `healthz` and `readyz` into `liveness` and `readiness`; update the
   route table and the discovery test's receiver rule.
5. Change `route.method` to `route.methods`, update `buildMux`, and update the
   discovery fixture and its string replacements.
6. Re-run the full suite, including the DSN-backed integration tests, the race
   suite, the outage log, and the cheats. The type changes should not alter
   observable behavior; every existing property check must stay green.

### Test changes that are not behavior changes

- `isbn` table tests become `got, err := Parse(input)`, compare
  `got.String()` to `want`, and on error assert `got.Valid() == false` and
  `got.String() == ""`.
- `FuzzCanonicalIdempotent` becomes `first, _ := Parse(s)` then
  `second, _ := Parse(first.String())` and compares `first == second`.
- `stored` fixtures and fake stores use `book.New`; if a helper is needed in
  tests, define a local `mustBook(t, ...)` rather than adding an exported
  `MustNew` to the domain package.
- Fake `List` implementations return `book.NewList(...)`.
- Assertions that compare `book.Book` values can use `==` (all fields are
  comparable). Assertions that compare lists use `All()` and compare slices.
- The discovery fixture's `route` shape and the two `strings.Replace` patterns
  that name `route{method: ...}` must change with `route.methods`. The
  eleven negative cases must still fail for the same reasons.

## What this design does not claim

- It does not make persistence, idempotence, concurrency, timeout, or
  liveness type-enforced. Those are operational properties.
- It does not remove Go's zero values. `isbn.ISBN{}` and `book.Book{}` remain
  constructible; `Valid` is a runtime guard, not a proof.
- It does not seal `httpapi` from itself. Moving `failure` to `problem` seals
  the error vocabulary from handler code; it does not stop same-package code
  from adding a raw writer. The discovery and structural checks cover that
  gap.
- It does not make `api.handler_registration` type-enforced. Go has no
  nominal handler type; the discovery check is the enforcement.
- It does not change the ledger, the contract, the scenarios, or the property
  statements. Where the contract and the current code disagree (the
  `internal` error code), this document records the gap for the owner rather
  than resolving it here.
