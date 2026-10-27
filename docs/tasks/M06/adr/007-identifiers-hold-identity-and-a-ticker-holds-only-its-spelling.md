# Identifiers hold identity and a ticker holds only its spelling

A `mic_ticker` row in `identifiers` names the instrument its ticker identifies today.
`PlaceTicker` moves the row when an identity answer names another instrument. Ticker
validity answers the same question with dates, from the identifier facts. Nothing
reconciles the two. An identifier attestation, a reorder of datasource precedence or an
identifier facts fetch changes ticker validity. The row stays where it is. Resolution
never matches through a ticker row. The row only names a holding and carries a listing's
venue.

`identifiers` holds identity only: the stable identifiers, option symbols and broker
descriptions. Resolution may match through every row. A CHECK refuses `mic_ticker`.

A ticker is a row of `tickers (id, market, value)`, in the system's spelling. The table
has `UNIQUE (market, value)` and `UNIQUE (id, market)`. The second lets another table hold
a composite foreign key that keeps a ticker in its market. The row holds only the
spelling. One get-or-create function writes the table. It is the only place that checks
the spelling. A row may outlive its facts.

Every fact, `corp_act_answers`, the projection of [011](011-an-instruments-ticker-is-a-projection-of-ticker-facts.md)
and a dated association refer to a ticker by `ticker_id`. Statements and facts still meet
by value. A stated key keeps its identifiers as the statement gave them. A new fact
finds the keys that state its ticker through the GIN index.

`identifier_assertions` holds ticker assertions only. A stable identifier needs none,
because its `identifiers` row is the claim. An option symbol and a bare ticker get none.
For each of them the fetch key already records the identifier sent, the outcome and the
count of candidates.

`markets` holds one row per market, keyed by its code. `market_members` lists every
operating MIC with its market, including an operating MIC that forms a market alone. It
has `UNIQUE (operating_mic, market)`. `mic_markets` becomes a plain join. The market
columns of `tickers`, `identifier_period_assertions` and `market_members` are foreign keys
to `markets`.

An integration converts its own ticker spelling to a `mic_ticker` as it converts an
answer. It drops a ticker that it cannot convert. The answer contributes its stable
identifiers either way. `openfigi_ticker` leaves the enum. `datasource_ticker` remains so
that `fetch_keys.sent_type` can record the spelling a request sent, as EODHD's splits
fetch sends `BRK-B.US`. Statement validation refuses both types. A key cannot resolve
through either.

## Considered options

- Associate identity through tickers, and rely on replay to realign after a
  reassignment. A stated key is undated. A key that spans a reassignment therefore gets one
  instrument however often it is replayed. Replay re-resolves keys. It leaves
  `instruments`, `listings` and `identifiers` as they are. A merge through a reused ticker
  stays.
- Keep stable and MIC-derived identifiers in two tables. Option symbols are MIC-derived,
  but resolution matches through them, so the line falls between identity and tickers
  rather than between reassignment traits.
- Keep the (market, value) natural key in every table. It avoids a lookup. Each writer
  checks the spelling. The schema does not say what a ticker is.
