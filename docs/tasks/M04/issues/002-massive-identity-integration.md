---
title: Massive identity integration
type: task
---

## Scope

An integration that answers from Massive what a source states about an instrument: the
identifiers, asset class, currency and venue of each listing a stated identifier names.

In:

- The client for Massive, tested against recorded traffic redacted as it is saved.
- The declaration of the identifier types and domains it serves, and the identifier it
  sends for each.
- Answering only for the venues and asset classes Massive covers: US listed stock, ETFs
  and funds. See
  [005](../adr/005-massive-and-eodhd-serve-listed-stock-etfs-and-funds.md).
- Conversion of each answer to the canonical candidate shape. A venue is returned as the
  domain of a MIC_TICKER, normalised to its operating MIC. Every identifier Massive gave a
  candidate is returned, and candidates are not ranked.
- The classification of Massive's errors as temporary or permanent, and its rate limit.
- The declaration of whether each identity integration lists every listing of an
  instrument, and the contradiction check that reads it.
- A datasources row and the credential and config it carries, so an administrator can
  enable the integration from the admin area.
- End to end coverage through vcrproxy of a key that OpenFIGI and Massive both answer.
- One vcrproxy serving every provider by hostname.

Out:

- Prices, corporate events and identifier events, whatever Massive could serve.
- Options. Resolution of an OCC symbol needs corporate event coverage of the underlying,
  which the instrument resolution note describes.

## Design

Massive answers from `/v3/reference/tickers/{ticker}`, whose only key is a ticker. The
record carries the ticker, the primary exchange as a MIC, the composite and share class
FIGIs, the currency, the type and whether the ticker is active. It omits ISIN, CUSIP
and SEDOL. A CUSIP is looked up through the `cusip` filter of
`/v3/reference/tickers`, which accepts a CUSIP and never returns one. The only FIGI
lookup is an experimental events endpoint that returns a ticker and needs a second call,
and it is not used.

The integration sends:

- a MIC_TICKER whose domain is empty or a US operating MIC, or an OPENFIGI_TICKER under
  OpenFIGI's US composite code, as the ticker;
- a CUSIP as the `cusip` filter.

Massive is not asked for a key that states only an ISIN, SEDOL, CINS, Wertpapier, a FIGI
of either kind, or a ticker at a venue outside Massive's table of venues. See
[003](../adr/003-each-provider-maps-its-venues-through-a-generated-table.md).

Massive spells a class share with a dot (BRK.B) and a preferred share with a lowercase p
and the series letter (ABRpD). An unknown ticker returns a 404, or a 200 with no
results, and both are an empty answer.

An answer is one candidate, the listing at the primary exchange. It carries the share
class FIGI, the composite FIGI and a MIC_TICKER at the primary exchange normalised to its
operating MIC. The currency is `currency_name` upper cased. The class comes from `type`:
CS, PFD, OS, ADRC, ADRP, ADRR, GDR and NYRS are stock; ETF, ETN, ETV and ETS are etf;
FUND and BASKET are mutual_fund. A record converts to a candidate only when its type maps
to a class and its market is `stocks`; see
[005](../adr/005-massive-and-eodhd-serve-listed-stock-etfs-and-funds.md). The live list
of codes at `/v3/reference/tickers/types` grows, so a recorded test fails when the live
list has a code that the integration neither converts nor declares unconverted. The CIK
is discarded; the deferred companies note records it.

The credential is sent as the `apiKey` query parameter. Massive answers one key per
request, so a batch is one key. The rate comes from the plan the datasources row's config names,
and errors are classified as [006](../adr/006-massive-and-eodhd-rates-and-failures.md)
describes.
