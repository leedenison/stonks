---
title: Standard abbreviations
type: task
---

## Scope

Code names use a standard abbreviation for each long word in common use.

| Word | Abbreviation |
|---|---|
| corporate | `corp` |
| corporate action | `corp act` |
| statement | `stmt` |
| instrument | `instr` |
| transaction, in the financial sense | `txn` |

A database transaction keeps `tx`.

The abbreviations apply to identifiers in Go, TypeScript, proto and SQL, and to URL
paths. They apply to compounds and plurals as well: `InstrumentID` becomes `InstrID`,
`instrument_id` becomes `instr_id`, and `ListInstruments` becomes `ListInstrs`. Prose,
comments and UI labels keep the full words.

In:

- `docs/abbreviations.md` lists the abbreviations and the rules above.
- The Naming section of `CLAUDE.md` points at `docs/abbreviations.md`.
- Every name in the code that holds one of the words. The migrations change in place.
