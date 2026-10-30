# 006: store reports an absent delete as `ErrNotFound`

**Reintroduction.** `Store.Delete` inspects `RowsAffected` and returns
`ErrNotFound` when nothing was deleted. The handler maps `ErrNotFound` to 204,
so HTTP idempotence survives, but the storage contract now reports absence as an
error.

**Must be rejected by.** `TestDeleteAbsentReturnsNil`,
`TestDeleteTwiceReturnsNil`, and `TestConcurrentDeletesRemoveRow`.

**Expected verdict.** Rejected. This control keeps the handler's tolerant 204
from hiding a store-level regression.
