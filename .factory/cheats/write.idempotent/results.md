# Results: upsert-never-updates mutation

Recorded 2026-10-29 with Go 1.27.2 darwin/arm64 and PostgreSQL 16.15.

| Mutation | Command | Verdict | Failing test |
|---|---|---|---|
| `001-upsert-does-nothing.patch` (`ON CONFLICT ... DO NOTHING`) | `go test ./httpapi -run TestPutIsIdempotentAndLastWriteWins -count=1` | rejected | `TestPutIsIdempotentAndLastWriteWins` |
| same | `go test ./store -run TestPutTwiceLastWriteWins -count=1` | rejected | `TestPutTwiceLastWriteWins` |

The HTTP-level failure reads the row back through `Store.Get`, after the
response has already echoed the updated record:

```
--- FAIL: TestPutIsIdempotentAndLastWriteWins
    stored record = {ISBN:9781124377797 Title:The C Programming Language ... PublishedYear:1988},
    want {ISBN:9781124377797 Title:Updated Title ... PublishedYear:1999}
```

After reversing the patch, `BOOKAPI_TEST_DSN=... go test ./... -count=1`
passed.
