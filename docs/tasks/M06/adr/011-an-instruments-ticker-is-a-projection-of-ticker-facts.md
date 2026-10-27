# An instrument's ticker is a projection of ticker facts

Ticker validity answers what a ticker named on a day. Naming a holding asks the reverse: what an
instrument is called today, at each venue. The views `instr_names` and
`listing_tickers`, the holdings, the admin search and a dated key's fallback listing all
ask it. `Derive` runs in Go. A SQL view therefore cannot derive the answer on read.

`resolved_tickers` stores the answer. Its columns are `ticker_id`, `market`,
`instr_id`, `venue`, `valid_from`, `valid_before` and `currency`. The rows of one
instrument and market are unique by venue. Only the rebuild from the facts writes
the rows. The projection holds names only. Holdings, dating and corporate actions
still read ticker validity from the facts.

For each instrument and market:

1. The ticker is the one whose last interval naming the instrument ends latest. An open
   interval ends latest. Confirmed and provisional intervals both count.
2. A tie goes to the ticker with the latest winning assertion that names the instrument,
   and then to the lower value. When only identity answers report a rename, ties are
   common. Both tickers stay open.
3. Only the chosen ticker gets rows. `valid_from` and `valid_before` bound the unbroken
   span of intervals that ends with the last one. A NULL `valid_before` means the ticker
   still names the instrument.
4. Each venue that an assertion of the pair names gets a row. Its currency comes from the
   most recent assertion at that venue that states one.
5. If every assertion of the pair omits the venue, one row holds the ticker with a NULL
   venue.

A row reaches a listing by joining its currency to the family of a listing of its
instrument. A row needs a currency to join. A listing that a later write creates is found
without a rebuild.

A ticker relates to an instrument as current, defunct or former. It is current when its
row of the instrument is open. It is defunct when another instrument has an open row of
it. It is former otherwise. A ticker that still names an instrument only through a tie
that it lost has no row for that instrument. It then reads as former on the instrument it
named before. The relation is decided on read, in one view.

A holding shows a defunct ticker as `ABC (defunct)`. It shows a former ticker plainly,
so a delisted holding keeps its name. The admin tickers tab labels every relation.

An instrument's name in a list comes from a row that states a venue and is not defunct.
The name is an identifier, whose domain is an operating MIC, so a row without a venue
cannot name the instrument there. A defunct ticker belongs to its present holder.

A dated key's transactions sit under the instrument's listing in the family of the key's
currency. Failing that, they sit under the listing that the `resolved_tickers` row for the
key's ticker reaches at `via_venue`.

The rebuild runs in the transaction that writes facts: the identity write, `Record`, an
identifier attestation or its withdrawal, and a merge. A rebuild of every ticker runs as a
batched `rebuild_resolved_tickers` system run. A change of datasource precedence starts
it. An administrator starts it after a deploy that changes this rule, or after a deletion.
A deleted user or run cascades to its facts. The run repairs the projection. Names may lag
while the run works.

## Considered options

- Derive the names on read in Go. The SQL views and the admin search would move into Go.
  Every holdings read would derive every held ticker.
- A delete trigger that repairs the projection. A trigger cannot run `Derive`.
