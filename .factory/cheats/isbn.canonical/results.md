# Results: ISBN prefix-guard mutation

Recorded 2026-10-29 with Go 1.27.2 darwin/arm64.

The fixtures now use `9770131103628` and `977-0-13-110362-8`. Both have valid
ISBN-13 check digits, so only the 978/979 prefix rule rejects them.

| Mutation | Command | Verdict | Failing tests |
|---|---|---|---|
| `001-no-prefix-guard.patch` (delete the 978/979 guard) | `go test ./... -count=1` | rejected | `TestCanonical/977_prefix_with_valid_checksum`, `TestCanonical/977_prefix_with_valid_checksum_and_hyphens`, `TestHandlers/get_977_prefix_with_valid_checksum` |

Observed failures:

```
--- FAIL: TestCanonical/977_prefix_with_valid_checksum
    isbn_test.go: Canonical("9770131103628") error = <nil>, want ErrInvalid
--- FAIL: TestCanonical/977_prefix_with_valid_checksum_and_hyphens
    isbn_test.go: Canonical("977-0-13-110362-8") error = <nil>, want ErrInvalid
--- FAIL: TestHandlers/get_977_prefix_with_valid_checksum
    httpapi_test.go: status = 404, want 400
```

After reversing the patch, `BOOKAPI_TEST_DSN=... go test ./... -count=1`
passed.
