# Datasource precedence picks the price for each day

Massive, EODHD and Google Sheets each serve prices and FX rates. The plans on hand are
Massive Basic and EODHD free, and each is believed to serve only the most recent years.
Google Sheets serves the years before them. A single source per market, the rule for
corporate actions, would leave most of a long holding without a price.

So the source is chosen for each listing on each day, and one series may mix sources:

* Each price row carries its datasource, and the system stores every answer. A read takes
  the row of the highest ranked datasource for the day. A change of precedence applies to
  the stored rows without a refetch.
* A fetch asks the datasources in precedence order. Each is asked for the days that the
  datasources above it left uncovered. A datasource leaves each day outside its window
  uncovered.
* Google Sheets ranks below Massive and EODHD.
* Each price references the fetch key that produced it. The fetch key records the ticker
  sent and the moment of the fetch.

A mixed series is sound only when every source is on one basis. See
[003](003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).

## Considered options

* Comparing two sources for one day and raising a finding when they disagree. Two sources
  rarely answer for one day under the gap rule, so the comparison is left out.
* Asking only for the days inside the validity period of the ticker sent. A provider
  serves the history of an instrument under its current symbol, so the restriction would
  lose the prices from before a rename.

## Consequences

* After a lower ranked datasource covers a day, a fetch does not ask a higher ranked one
  for it. A plan upgrade therefore does not replace the prices that Google Sheets
  supplied.
* An identifier edge between the date of a price and the moment of its fetch does not
  cause a refetch in this milestone. Where a provider serves a reused ticker, part of the
  history under the symbol may belong to an earlier company. The system stores and shows
  those prices as if they were sound. The fetch key holds what the later unwinding needs.
