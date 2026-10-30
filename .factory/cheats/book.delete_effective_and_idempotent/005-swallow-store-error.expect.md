# 005: store error is swallowed

**Reintroduction.** The handler discards `Store.Delete`'s error and answers
204, so an unavailable database looks like a successful delete.

**Must be rejected by.** `TestDelete/store_error_returns_503_without_leaking_detail`
and `TestStoreTimeoutReturns503/DELETE` (including the 2 second bound).

**Expected verdict.** Rejected.
