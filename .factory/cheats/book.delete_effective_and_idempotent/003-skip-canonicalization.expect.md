# 003: delete skips canonicalization

**Reintroduction.** The path value is validated but forwarded raw to the store,
so an ISBN-10 DELETE answers 204 and leaves the ISBN-13 row in place.

**Must be rejected by.** `TestDelete/isbn-10_path_deletes_canonical_isbn-13`
(the store must receive the canonical ISBN-13) and
`TestDeleteIntegrationISBN10` (the canonical row must be gone and both forms
must read 404).

**Expected verdict.** Rejected.
