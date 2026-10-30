# 001: upsert never applies an update

**Mutation.** Change `Store.Put` from `ON CONFLICT ... DO UPDATE` to
`ON CONFLICT ... DO NOTHING`. The handler still echoes the request and
`COUNT(*)` stays one, so response-only checks pass.

**Must be rejected by.** `TestPutIsIdempotentAndLastWriteWins` in
`httpapi/integration_test.go`, which now reads the stored record after the
update and requires it to contain the updated title, author, and year. The
store-level `TestPutTwiceLastWriteWins` also rejects it.

**Expected verdict.** Rejected with `BOOKAPI_TEST_DSN` set.
