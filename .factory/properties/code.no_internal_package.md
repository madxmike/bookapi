# `code.no_internal_package`

## Property

The module contains no directory named `internal`, at any depth.

## Definition

- The name is exactly `internal`, case-sensitive.
- A directory or a symlink named `internal` at any depth violates it.
- `.git` is excluded.
- `Internal`, `.internal`, and a regular file named `internal` are not violations.

## Witness

Any one proves it false:

- a directory named `internal` exists outside `.git`;
- a Go package path contains `/internal/`.

## Evidence required

- A check that fails on the current tree and passes after the change.
- The check runs as part of `go test ./...`.
- Demonstrate the negative control: the check fails before the change and lists the
  violating path.
- The check rejects two independent reintroductions of the violation, one of them
  nested under an existing package.
- Record the evidence class and the artifact in `.factory/evidence.md`.

The implementer chooses the form of the check and the shape of the change.

## Acceptance

- `go test ./...` passes.
- The check rejects a restored top-level `internal/`.
- The check rejects a nested `<pkg>/internal/`.
- Existing behavior does not change and the existing suite stays green.

## Non-goals

- Renaming or hiding packages beyond what the property requires.
- Visibility changes not implied by the move.
- Capitalized or hidden variants.

## Constraints

- Do not weaken or delete existing tests.
- Do not add dependencies.
- Do not change the property statement, witness, or polarity.
- Record any design decision the spec does not cover in the ledger rather than
  deciding silently.
