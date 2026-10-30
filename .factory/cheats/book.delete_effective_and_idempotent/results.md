# Results: delete cheat corpus

Recorded 2026-10-29.

Environment: Go 1.27.2 darwin/arm64; PostgreSQL 16.15 on
`127.0.0.1:55432`; `BOOKAPI_TEST_DSN` set, so every DB-backed test ran.

Procedure for each row: apply the patch from the repository root, run
`go test ./... -count=1` with the DSN set, reverse the patch, and confirm the
suite is green again.

| # | Reintroduction | Verdict | Failing tests |
|---|---|---|---|
| 001 | DELETE does nothing and answers 204 | rejected | `TestDelete/existing_book_returns_204_with_empty_body`, `TestDelete/get_after_delete_returns_404`, `TestDelete/isbn-10_path_deletes_canonical_isbn-13`, `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503/DELETE`, `TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationISBN10`, `TestDeleteIntegrationConcurrent` |
| 002 | DELETE pre-reads and returns 404 for an absent row | rejected | `TestDelete/absent_book_returns_204`, `TestDelete/repeat_delete_returns_204`, `TestDelete/store_reporting_absence_still_returns_204`, `TestDeleteIntegrationAbsent`, `TestDeleteIntegrationEffectAndIdempotence`, `TestDeleteIntegrationConcurrent` |
| 003 | DELETE forwards the raw path value | rejected | `TestDelete/isbn-10_path_deletes_canonical_isbn-13`, `TestDeleteIntegrationISBN10` |
| 004 | 204 carries a JSON body and content type | rejected | `TestDelete/existing_book_returns_204_with_empty_body`, `TestDelete/repeat_delete_returns_204`, `TestDelete/absent_book_returns_204`, `TestDelete/store_reporting_absence_still_returns_204`, `TestDelete/request_body_is_ignored`, and the integration empty-body checks |
| 005 | DELETE swallows the storage error | rejected | `TestDelete/store_error_returns_503_without_leaking_detail`, `TestStoreTimeoutReturns503/DELETE` |
| 006 | Store reports an absent delete as `ErrNotFound` | rejected | `TestDeleteAbsentReturnsNil`, `TestDeleteTwiceReturnsNil`, `TestConcurrentDeletesRemoveRow`; DB-backed tests also fail because their cleanup treats `ErrNotFound` as an error |

After every reversal `go test ./... -count=1` passed with the DSN set.

`003` is a behavioral rejection: the store receives `0131103628`, so it
deletes no row, and the canonical `9780131103627` row remains. The first
generation of that patch only failed to compile, so it was retargeted at
`handleDelete` before being recorded here.
