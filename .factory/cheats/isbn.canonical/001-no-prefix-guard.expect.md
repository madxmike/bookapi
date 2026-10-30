# 001: remove the 978/979 prefix guard

**Mutation.** Delete the prefix check from `isbn.Canonical`. Checksum-valid
`9770131103628` then canonicalizes as if it were an ISBN.

**Must be rejected by.** `TestCanonical/977_prefix_with_valid_checksum` and
`TestCanonical/977_prefix_with_valid_checksum_and_hyphens` in the ISBN package,
and `TestHandlers/get_977_prefix_with_valid_checksum` at the HTTP level (it
would return 404 for a missing book instead of 400 `invalid_isbn`).

**Why the fixture change matters.** The old fixtures ended in `6` and `7`, so
the checksum rejected them even with the prefix guard removed. `9770131103628`
is checksum-valid; only the prefix rule rejects it.

**Expected verdict.** Rejected.
