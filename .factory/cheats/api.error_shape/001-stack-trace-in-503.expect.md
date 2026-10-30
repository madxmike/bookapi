# 001: 503 body includes a stack trace

**Mutation.** `writeError` appends `debug.Stack()` to every `unavailable`
message. The body contains no `pq:`, `SQLSTATE`, `/etc/postgresql`, or
`connection refused`, so the old substring grep would accept it.

**Must be rejected by.** The exact envelope assertions in
`TestHandlers/get_store_error`, `TestHandlers/put_store_error`,
`TestStoreTimeoutReturns503`, and
`TestDelete/store_error_returns_503_without_leaking_detail`. Each requires the
message to be exactly `the book store is unavailable` and the envelope to have
only `error.code` and `error.message`.

**Expected verdict.** Rejected.
