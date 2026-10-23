# A transaction's instrument is derived when read

A transaction of a dated key takes its instrument from its ticker's validity at its order
date, derived at each read, as
[009](009-validity-is-kept-in-days-and-derived-when-read.md) derives validity itself.
Nothing stores it.

Holdings read in two parts. The transactions of fixed keys are summed by instrument in
SQL, as before. The transactions of dated keys are listed with their ticker, order date,
quantity and currency. The reader derives validity once for each distinct ticker, gives
each transaction the instrument and listing the ticker names on its date, and merges the
sums. A transaction is unresolved where the ticker names nothing on its date. The
statement details page reads the same way. Holdings and statements take the validity
queries as a dependency.

A fixed key costs one SQL sum, as before, and nothing is derived for it. A dated key
costs one derivation per distinct ticker, over a handful of indexed rows, however many
transactions rest on it.

## Considered options

- An `instrument_id` on each transaction of a dated key, recomputed when facts change. It
  is wrong after a datasource reorder or a fetch key cascade until something recomputes
  it, and every write of facts fans out to transaction rows.
- A table of each ticker's validity intervals, joined by date range in SQL. It is the
  cache [009](009-validity-is-kept-in-days-and-derived-when-read.md) rejected.

## Consequences

The per-instrument summary that valuation would read stays out of this milestone, as
issue [001](../issues/001-open-m05.md) states. Nothing here forecloses it.
