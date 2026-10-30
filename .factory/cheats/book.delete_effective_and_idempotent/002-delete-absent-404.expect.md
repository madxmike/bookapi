# 002: delete returns 404 for an absent row

**Reintroduction.** `handleDelete` reads the row first and returns 404
`not_found` when it does not exist. This looks like normal resource handling but
makes a repeated DELETE fail.

**Must be rejected by.** `TestDelete/absent_book_returns_204`,
`TestDelete/repeat_delete_returns_204`,
`TestDelete/store_reporting_absence_still_returns_204`,
`TestDeleteIntegrationAbsent`, the second delete in
`TestDeleteIntegrationEffectAndIdempotence`, and
`TestDeleteIntegrationConcurrent`.

**Expected verdict.** Rejected, with or without a database.
