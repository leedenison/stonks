# A ticker assertion records the venue and currency its source stated

Ticker validity is derived at the market. Naming an instrument at a venue, and placing a
ticker on a listing, needs the venue and the currency. Today the ticker row in
`identifiers` carries the venue and its listing.
[007](007-identifiers-hold-identity-and-a-ticker-holds-only-its-spelling.md) removes that
row.

An assertion records `venue` and `currency` as its source stated them. Both are nullable.
Derivation ignores them. The projection of [011](011-an-instruments-ticker-is-a-projection-of-ticker-facts.md)
reads them.

| Source | venue | currency |
|---|---|---|
| EODHD identity answer | the operating MIC of the row's venue | the row's currency |
| OpenFIGI identity answer | the operating MIC of the exchange code | the request's currency filter, NULL where the key states none |
| Massive ticker history and point in time lookup | NULL | NULL |
| Identifier attestation | NULL | NULL |

An answer that lists one ticker at several venues of its market writes one assertion per
venue. OpenFIGI lists a US ticker at many exchange codes, and the projection gives each
venue its own row. The unique key of `identifier_assertions` therefore includes `venue`.

OpenFIGI's answer omits the currency. Its integration sends the key's currency as a
filter. It stamps that currency on every listing it returns. The currency is therefore a
property of the request.

An assertion carries `market` beside `ticker_id` and `venue`. Two composite foreign keys
hold the venue in the ticker's market: `(ticker_id, market)` references `tickers (id,
market)`, and `(venue, market)` references `market_members (operating_mic, market)`.
`stated_keys` holds the same pair through `via_ticker_id`, `via_market` and `via_venue`.
