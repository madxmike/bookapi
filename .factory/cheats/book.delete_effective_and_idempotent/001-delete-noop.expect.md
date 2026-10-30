# 001: delete is a no-op

**Reintroduction.** `handleDelete` validates the ISBN but never calls
`Store.Delete`; every DELETE is answered 204.

**Must be rejected by.** `TestDelete/existing_book_returns_204_with_empty_body`,
`TestDelete/get_after_delete_returns_404`,
`TestDelete/isbn-10_path_deletes_canonical_isbn-13`,
`TestDelete/store_error_returns_503_without_leaking_detail`,
`TestStoreTimeoutReturns503/DELETE`, and every DB-backed delete test that reads
the row after the response.

**Expected verdict.** Rejected. Response shape alone cannot distinguish this
from a real delete, so the checks read storage.
