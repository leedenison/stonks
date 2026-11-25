# A fetch asks only for gaps, and a failed fetch leaves one

## What starts a fetch

* An administrator starts a system run, which asks for every gap. Until the schedule
  trigger of D-RUNS exists, this run is what keeps an open holding current.
* An ingest that adds a holding starts a fetch for its prices. An ingest can also bring a
  currency into use or move the first day held earlier, so it starts the rate fetch too.
* A user who selects a display currency starts a rate fetch. The fetch asks only when the
  currency is new to the system, and the series then serves every user.

A price fetch needs the resolved instrument and its ticker, so it follows the resolve run
of the ingest. The corporate actions fetch for the same tickers also runs first.

A run asks Massive and EODHD. A fetch run never asks Google Sheets: its tool asks the
server for the gaps that remain when an administrator runs it. See
[007](007-the-google-sheets-tool-writes-formulas-and-reads-pasted-values.md).

## What a failed fetch leaves

When a fetch fails, it leaves every day it asked for uncovered, as for every datasource.
The stored prices stay, and the days stay gaps. The next run or ingest asks for them
again, so the gaps are the only retry state. A failure is per key: one ticker that fails
leaves the prices of every other ticker in the run stored.

A failure passes the gap down the precedence, so a temporary failure at one datasource
can be filled by the next in the same run.

When a price fetch fails, the run records its failed keys as every run does. The system
does not raise an alert beyond that record.
