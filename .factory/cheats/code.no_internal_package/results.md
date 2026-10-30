# Results: no-internal controls

Recorded 2026-10-29 with Go 1.27.2 darwin/arm64.

Command: `go test ./cmd/api -run TestNoInternalPackage -count=1`.

| Control | Tree | Verdict | Reported path |
|---|---|---|---|
| baseline | regenerated tree | pass | — |
| `001-top-level-internal.patch` | `internal/keep.go` | reject | `internal` |
| `002-nested-internal.patch` | `httpapi/internal/x.go` | reject | `httpapi/internal` |
| regular file named `internal` | `<root>/internal` | pass | — |
| directory named `Internal` | `<root>/Internal` | pass | — |
| directory named `.internal` | `<root>/.internal` | pass | — |
| symlink named `internal` | `<root>/internal -> book` | reject | `internal` |
| after cleanup | regenerated tree | pass | — |

The two patch controls are independent reintroductions: one at the module root
and one nested under an existing package. The regular-file, capitalized, and
hidden-name controls confirm the check does not over-reject values that the
property definition says are allowed.
