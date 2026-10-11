# The database ranks identifier types for naming an instrument

The admin instruments list pages through every instrument in name order. An instrument
has no name column. The client names an instrument by its identifiers, taking the first
of a preferred order of types, with the currency code first for cash and the option
symbol first for an option. Paging in name order needs the name in the query.

`identifier_type_traits` has a `name_rank` column, unique across the types. The list
query names an instrument by the value of its identifier of the lowest rank, over its
own identifiers and its listings' together. Among tickers it prefers the listing's
primary venue, then the venue ranked first in `venue_names`, then the lowest MIC, as the
holdings query does. The query orders by that name then by id, and the response carries
the name, so the admin page shows what the page was ordered by.

One global rank covers every class. The OCC symbol, the option symbol and the currency
code rank first. Only an option carries an OCC or option symbol, and only cash carries a
currency code, so the rank names every instrument without a per-class rule.

## Consequences

The holdings page names a holding by the ticker the holdings service chooses for the
held listing, and falls back to the client's preferred order over the instrument's own
identifiers. That fallback order is a second copy of the rank. The trait test holds the
Go copy of the table equal to the table; nothing holds the client copy. The orphan
issue `name-a-holding-from-the-database-rank` records the follow-up that removes it.
