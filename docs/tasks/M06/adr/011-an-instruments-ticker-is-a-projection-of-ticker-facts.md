# An instrument's ticker is a projection of ticker facts

Validity answers what a ticker named on a day. Naming a holding asks the reverse: what an
instrument is called today, at each venue. The views `instrument_names` and
`listing_tickers`, the holdings, the admin search and a dated key's fallback listing all
ask it. `Derive` runs in Go. A SQL view therefore cannot derive the answer on read.

`resolved_tickers` stores the answer. Its columns are `ticker_id`, `instrument_id`,
`venue`, `since`, `until` and `currency`. Only the rebuild from the facts writes
the rows. The projection holds names only. Holdings, dating and corporate actions
still read validity from the facts.

For each instrument and market:

1. The ticker is the one whose last interval naming the instrument ends latest. An open
   interval ends latest. Confirmed and provisional intervals both count.
2. A tie goes to the ticker with the latest winning assertion that names the instrument,
   and then to the lower value. When only identity answers report a rename, ties are
   common. Both tickers stay open.
3. Only the chosen ticker gets rows. `since` and `until` bound the unbroken span of
   intervals that ends with the last one. A NULL `until` means the ticker still names the
   instrument.
4. Each venue that an assertion of the pair names gets a row. Its currency comes from the
   most recent assertion at that venue that states one.
5. If every assertion of the pair omits the venue, one row holds the ticker with a NULL
   venue.

A row reaches a listing by joining its currency to the family of a listing of its
instrument. A row needs a currency to join. A listing that a later write creates is found
without a rebuild.

A ticker relates to an instrument as current, defunct or former. It is defunct when it
now names a different instrument. The UI shows it as `ABC (defunct)`. It is former when
it names nothing. The UI shows a former ticker plainly, so a delisted holding keeps its
name. The views decide the relation on read.

A dated key's transactions sit under the instrument's listing in the family of the key's
currency. Failing that, they sit under the listing that the `resolved_tickers` row for the
key's ticker reaches at `via_venue`.

The rebuild runs in the transaction that writes facts: the identity write, `Record`, an
attestation or its withdrawal, and a merge. A rebuild of every ticker runs as a batched
`rebuild_resolved_tickers` system run. A change of datasource precedence starts it. An
administrator starts it after a deploy that changes this rule, or after a deletion. A
deleted user or run cascades to its facts. The run repairs the projection. Names may
lag while the run works.

## Considered options

- Derive the names on read in Go. The SQL views and the admin search would move into Go.
  Every holdings read would derive every held ticker.
- A delete trigger that repairs the projection. A trigger cannot run `Derive`.
