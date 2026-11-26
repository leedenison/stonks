# A fetch asks only for gaps, and a failed fetch leaves one

## What starts a fetch

* An administrator starts a system run, which asks for every gap. Until the schedule
  trigger of D-RUNS exists, this run is what keeps an open holding current.
* An ingest starts a fetch for every gap in the prices of the listings that its user
  holds. An ingest can also bring a currency into use or move the first day held
  earlier, so the fetch asks for every gap in the rates too. An ingest starts a fetch
  only when such a gap exists.
* A user who selects a display currency starts a rate fetch. The fetch asks only when the
  currency is new to the system, and the series then serves every user.

A price fetch needs the resolved instrument and its ticker, so it follows the resolve run
of the ingest. The corporate actions fetch for the same tickers also runs first.

A run asks Massive and EODHD. A fetch run never asks Google Sheets: its tool asks the
server for the gaps that remain when an administrator runs it. See
[007](007-the-google-sheets-tool-writes-formulas-and-reads-pasted-values.md).

The run of an administrator reads past the fetch cache, so each of its requests spends
quota. The fetch after an ingest reads the cache.

## What a failed fetch leaves

When a fetch fails, it leaves every day it asked for uncovered, as for every datasource.
The stored prices stay, and the days stay gaps. The next run or ingest asks for them
again, so the gaps are the only retry state. A failure is per key: one ticker that fails
leaves the prices of every other ticker in the run stored.

A failure passes the gap down the precedence, so a temporary failure at one datasource
can be filled by the next in the same run.

When a price fetch fails, the run records its failed keys as every run does. The system
does not raise an alert beyond that record.

## Consequences

* The fetch after an ingest asks for more than the new holding. The days after the last
  price served are gaps for every open holding, so the fetch also asks for the recent
  days of each listing that the user already held. See
  [011](011-price-coverage-ends-at-the-last-price-served.md).
* A user who confirms a candidate adds a priced holding without an ingest. Its prices
  wait for the next ingest of the user or the next run of an administrator.
* The fetch after an ingest is a system run beneath the ingest run, so its prices are
  deleted with the user of the ingest. The days become gaps again, and the next run
  fetches them at the cost of the calls. The run of an administrator is a root run, and
  its prices outlive every user.
