# Results: stack-trace-in-503 mutation

Recorded 2026-10-29 with Go 1.27.2 darwin/arm64.

| Mutation | Command | Verdict | Failing tests |
|---|---|---|---|
| `001-stack-trace-in-503.patch` (append `debug.Stack()` to every `unavailable` message) | `go test ./... -count=1` | rejected | `TestHandlers/get_store_error`, `TestHandlers/put_store_error`, `TestStoreTimeoutReturns503/GET`, `/PUT`, `/DELETE`, `TestDelete/store_error_returns_503_without_leaking_detail` |

Observed failure, abbreviated:

```
--- FAIL: TestHandlers/get_store_error
    error message = "the book store is unavailable\ngoroutine 43 [running]:\n
    runtime/debug.Stack() ... /Users/madxmike/workplace/bookapi/httpapi/httpapi.go:193 ...",
    want one of the stable messages for "unavailable": [the book store is unavailable]
```

The injected text contains none of the four old greps (`pq:`, `SQLSTATE`,
`/etc/postgresql`, `connection refused`), so the earlier check would have
accepted it. The exact envelope requires the message to be one of the declared
strings and the body to contain only `error.code` and `error.message`.

After reversing the patch, `BOOKAPI_TEST_DSN=... go test ./... -count=1`
passed.
