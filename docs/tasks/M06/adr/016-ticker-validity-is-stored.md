# Ticker validity is stored

Every reader of what a ticker names on a day reads the ticker's facts and derives its
ticker validity. These readers are holdings, statement marks, corporate actions, the dated
path of a resolution and re-assertion. The derivation runs in Go, so holdings cannot read
ticker validity in a query. Each reader branches on whether a key is fixed or dated. Of
everything ticker validity implies, only the names in `resolved_tickers` are stored.

## Decision

Ticker validity is stored in `ticker_validity`, one row for each range of days over
which a ticker names one instrument. Readers never derive.

- Each write of facts rebuilds the ticker validity of its tickers in its own transaction.
  So do an identifier attestation, its withdrawal and a merge. Assertion coverage of a
  whole market rebuilds every ticker of that market.
- Three changes wait for the rebuild run: a reorder of the datasources, a change of
  identifier event source, and a deletion that cascades to facts.
- An exclusion constraint keeps one row per ticker on any day.
- The `txn_instrs` view gives each transaction its instrument and validity. A fixed key
  gives its instrument. A dated key gives the instrument that ticker validity names on
  the transaction's as-at date.

## Considered options

- Store the instrument on each transaction. Each rebuild and each change of association
  would then rewrite the transactions concerned, and the key and its transactions would
  have to agree.
- Store a per instrument summary of coverage and ticker validity. It answers for an
  instrument, so a reader that holds a ticker and a day still derives.
