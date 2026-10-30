# `code.no_internal_package`

## 1. Property and witness

**Property.** The module contains no directory named `internal`, at any depth.

**Witness** (any one proves it false):

- a directory named `internal` exists outside `.git`;
- a Go package path contains `/internal/`.

**Definition.** The forbidden name is exactly `internal`, case-sensitive. A directory
or symlink named `internal` at any depth violates the property. `.git` is excluded.
`Internal`, `.internal`, and a regular file named `internal` are not violations.

`kind: primitive`, `scope: repo`, `locus: code`, `polarity: forbid`.

## 2. Check contract

A Go test at `cmd/api/structure_test.go`, package `main`, named
`TestNoInternalPackage`.

- Resolve the module root from the test working directory: `cmd/api` runs with
  cwd set to the package directory, so the root is `../..`.
- Walk the tree with `filepath.WalkDir`.
- Skip `.git`.
- Report every entry whose name is exactly `internal` and is a directory or a
  symlink.
- Fail with the relative path of each violation.
- Must run under `go test ./...`.
- Standard library only.

The check must be written and run **before** the refactor. It must fail and list
`internal`. That failure is the negative control; without it the check is not
known to work.

## 3. Refactor contract

Move, without changing behavior:

| From | To |
|---|---|
| `internal/book` | `book` |
| `internal/isbn` | `isbn` |
| `internal/store` | `store` |
| `internal/httpapi` | `httpapi` |

Rewrite every import `example.com/bookapi/internal/<pkg>` to
`example.com/bookapi/<pkg>`. `store`'s embedded migration moves with the package,
so `//go:embed migrations/*.sql` continues to work.

The refactor must not change behavior, tests, or exported identifiers. The only
permitted edits are file moves and import paths.

## 4. Acceptance

- Before the refactor: `go test ./cmd/api -run TestNoInternalPackage` fails with
  `forbidden internal package: internal`.
- After the refactor: `go test ./...`, `go vet ./...`, and `gofmt -l` are clean,
  and `TestNoInternalPackage` passes.
- `find . -type d -name internal -not -path './.git/*'` prints nothing.

## 5. Cheats

Each must be rejected by `TestNoInternalPackage`:

1. Restore `internal/` with one placeholder file.
2. Add `httpapi/internal/`.

A cheat that is accepted means the check is too weak.

## 6. Non-goals

- Capitalized `Internal` (not a Go internal package).
- Hidden `.internal` (not a Go internal package).
- A regular file named `internal`.
- Visibility changes beyond the move.

## 7. Decisions

- `D12`: no `internal` directory; every package importable at a stable path.
- The restriction is enforced by a test, not by review.
