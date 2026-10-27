---
title: Tickers leave identifiers
type: task
dependencies: [011]
---

## Scope

`identifiers` holds identity only, and a user's choice fixes a key through a stable
identifier. See
[007](../adr/007-identifiers-hold-identity-and-a-ticker-holds-only-its-spelling.md) and
[013](../adr/013-a-users-choice-fixes-a-key-through-a-stable-identifier.md).

In:

- The OpenFIGI and EODHD integrations convert their ticker spellings to a `mic_ticker`,
  or drop a ticker they cannot convert.
- `openfigi_ticker` is removed from the enum, the traits, `serves.go`, `type.proto` (with
  the later values renumbered) and `identifier-chip.tsx`.
- Statement validation refuses `datasource_ticker`.
- A CHECK on `identifiers` refuses `mic_ticker`.
- Removed: `PlaceTicker`, the ticker branch of `identify`, `writer.tickers`,
  `datedThrough`, and the ticker fallback in `writer.via`.
- `RelinkIdentifiers` moves identity rows only. `Ticker()` becomes a test of the type.
- A confirmation fixes the key through a stable identifier of the candidate.
- A CHECK on `stated_keys` refuses the arbiter `user` on a dated key.
- `Listing.identifiers` and `InstrHolding.identifiers` drop tickers. Their
  comments state that they hold identity only. Tickers come from the projection fields of
  [011](../adr/011-an-instruments-ticker-is-a-projection-of-ticker-facts.md).
- `instrListedAs` in `e2e/helpers/api.ts`, `confirm-candidate.spec.ts` and the other
  specs that find an instrument by a `MIC_TICKER` identifier read the projection fields.
- Comments: the identifiers, listings and `identifier_type` text in
  `003_instrs.sql`, and the dating text in `004_stmts.sql`.

## Tests

- `convert_test.go` in OpenFIGI:
  - the composite code `US` is dropped;
  - a bond ticker with two `/` is dropped;
  - `BRK/B` at `UN` becomes `XNYS:BRK.B`.
- `convert_test.go` in EODHD: a preferred share is dropped, and so is a venue that
  several EODHD codes list.
- The replay tests expect converted answers.
- Resolve dbtests:
  - a key that states only a ticker is dated, and `identifiers` is unchanged;
  - a confirmation fixes the key through the candidate's ISIN;
  - a reassigned ticker leaves the old holder named by it, as defunct.
- DB dbtests for the CHECK on `identifiers` and for the arbiter CHECK on `stated_keys`.
- A statement service test that refuses to confirm a candidate that carries only a
  ticker.
