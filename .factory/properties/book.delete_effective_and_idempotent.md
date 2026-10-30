# `book.delete_effective_and_idempotent`

## Property

A DELETE of a valid ISBN removes the book; a repeated DELETE also succeeds; a GET
afterwards returns 404.

## Witness

Any one proves it false:

- a DELETE of a valid, absent ISBN returns a non-2xx;
- a row remains after a 2xx DELETE;
- a GET returns 200 after a successful DELETE;
- the second DELETE returns a non-2xx.

## Observable contract

- Request: `DELETE /books/{isbn}`.
- Success: 204 with an empty body and no `Content-Type`, whether or not the row existed.
- Invalid ISBN: 400 with code `invalid_isbn`; no stored record is affected.
- Storage unavailable: 503 with code `unavailable`, with no internal detail.
- After success, GET on the same ISBN returns 404 with code `not_found`.
- The ISBN-10 and ISBN-13 forms address the same record.

## Evidence required

- A check that fails before the change and passes after.
- The check rejects each of these reintroductions: a delete that does nothing; a
  delete that returns 404 for an absent row; a delete that skips canonicalisation;
  a delete that returns a body on 204; a delete that swallows a storage error.
- DB-backed evidence is required for the effect on storage.

## Acceptance

- Scenarios 13-20 in `.factory/scenarios.md` pass.
- A repeated DELETE is 204 and a subsequent GET is 404.
- Existing behavior is unchanged.

## Non-goals

- Soft delete, tombstones, restore, audit log, cascade, authorization, bulk delete.

## Constraints

- Do not change the property statement, witness, or polarity.
- Record any design decision the spec does not cover in the ledger.
