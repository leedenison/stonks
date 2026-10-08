---
title: EODHD identity integration
type: task
dependencies: [002]
---

## Scope

An integration that answers from EODHD what a source states about an instrument: the
identifiers, asset class, currency and venue of each listing a stated identifier names.

In:

- The client for EODHD, tested against recorded traffic redacted as it is saved.
- The declaration of the identifier types and domains it serves, and the identifier it
  sends for each.
- Answering only for the venues and asset classes EODHD covers: listed stock, ETFs and
  funds on its 70 exchanges. See
  [005](../adr/005-massive-and-eodhd-serve-listed-stock-etfs-and-funds.md).
- The mapping between EODHD's exchange codes and operating MICs, in both directions.
- Conversion of each answer to the canonical candidate shape. A venue is returned as the
  domain of a MIC_TICKER, normalised to its operating MIC. Every identifier EODHD gave a
  candidate is returned, and candidates are not ranked.
- The classification of EODHD's errors as temporary or permanent, and its rate limit.
- One handling of a provider's HTTP response in the market package, shared by OpenFIGI,
  Massive and EODHD: an error carrying the provider's name, the status and the start of
  the body, and a call that decodes a 200 and returns that error otherwise. Each
  integration keeps only the classification of its statuses and the headers it reads.
- A hold on the datasource for the whole Retry-After of a temporary datasource failure.
  While the hold lasts, the fetch framework fails each key without a call, so a spent
  daily quota waits for its reset.
- A datasources row and the credential it carries, so an administrator can enable the
  integration from the admin area.
- End to end coverage through vcrproxy of a key that OpenFIGI and EODHD both answer.

Out:

- Prices, corporate events and identifier events, whatever EODHD could serve.

## Design

EODHD answers through three endpoints, each costing one call on every plan. See
[002](../adr/002-eodhd-spends-three-calls-on-an-identity-answer.md).

- `/api/search/{query}` takes a ticker, a company name or an ISIN and returns one row
  per listing: code, EODHD exchange code, name, type, country, currency, ISIN and
  whether the listing is primary. It matches on the name as readily as on the code, so
  a row answers a ticker query only when its code equals the ticker. It searches active
  tickers only.
- `/api/id-mapping` looks up an ISIN, a composite FIGI, a CUSIP, an LEI or a CIK and
  returns the symbol with all five.
- `/api/exchange-symbol-list/US?symbols=...` returns the venue of each US symbol named,
  as a venue name such as NYSE, NASDAQ or BATS.

The integration sends:

- an ISIN to search, and to id-mapping for the composite FIGI;
- a CUSIP or a composite FIGI to id-mapping, then the ISIN it returns to search;
- a MIC_TICKER with a venue to search, filtered by the EODHD code of the venue's
  operating MIC, and a MIC_TICKER with no venue to search unfiltered;
- an OPENFIGI_TICKER as a MIC_TICKER, through the operating MIC of its exchange code.

EODHD is not asked for a key that states only a SEDOL, CINS, Wertpapier or share class
FIGI.

EODHD spells a class share with a hyphen (BRK-B). An unknown query returns an empty
array, and an unknown symbol a 404; both are an empty answer.

Each row of a search is a candidate. It carries a DATASOURCE_TICKER in EODHD's
namespace, such as AAPL.US, the ISIN, the composite FIGI where id-mapping returned one,
and a MIC_TICKER at the operating MIC of the row's exchange code. An exchange code other
than US maps to one operating MIC. The code US spans XNAS, XNYS, OTCM and XCBO, so a US
row's venue comes from the exchange symbol list. The tables that map codes and venue
names to operating MICs are described in
[003](../adr/003-each-provider-maps-its-venues-through-a-generated-table.md). The
currency is the row's. The class comes from the type: Common Stock and Preferred Stock
are stock, ETF is etf, and FUND and Mutual Fund are mutual_fund. A row converts to a
candidate only when its type maps to a class and its venue is an exchange; see
[005](../adr/005-massive-and-eodhd-serve-listed-stock-etfs-and-funds.md). The CIK and
LEI are discarded; the deferred companies note records them.

The credential is sent as the `api_token` query parameter, with `fmt=json`. EODHD answers
one key per request, so a batch is one key. [006](../adr/006-massive-and-eodhd-rates-and-failures.md)
describes how errors are classified and how a spent daily quota is held.
