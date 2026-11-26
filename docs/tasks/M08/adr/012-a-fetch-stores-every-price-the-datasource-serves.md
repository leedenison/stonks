# A fetch stores every price the datasource serves

Massive and EODHD answer a price request with the whole history that the plan of the key
serves, whatever days the request names. A fetch asks only for gaps. See
[006](006-a-fetch-asks-only-for-gaps-and-a-failed-fetch-leaves-one.md). Storing only the
days asked would discard most of each answer. A later demand for an earlier day would
then cost another call for the same history.

So a fetch stores the whole answer:

* The coverage of an answer is every day the datasource answered for. It runs from the
  start of the history the datasource states to the last price served, and the period
  asked does not limit it. See
  [011](011-price-coverage-ends-at-the-last-price-served.md).
* The write keeps the days that the datasource has not yet covered for the listing, as
  for every answer. See
  [002](002-datasource-precedence-picks-the-price-for-each-day.md).
* A run sends one request for each listing. The request names the days from the first
  gap of the listing to its last, and the fetch key records them.

The rule covers the datasources that a run fetches. An upload from Google Sheets stores
only the rows inside the gaps of each series. The tool builds each formula from the
gaps, and an administrator pastes the sheet by hand, so each row costs effort. See
[007](007-the-google-sheets-tool-writes-formulas-and-reads-pasted-values.md).

## Considered options

Sending one request across the gaps of a listing and storing only the gap days was the
alternative. It keeps the table to the days held. It pays for the same history again
whenever a holding reaches back to an earlier day.

## Consequences

* Once a datasource has answered for a listing, a later demand for an earlier day inside
  that history finds the day covered and is served without a call.
* A higher ranked datasource that is asked for any gap of a listing also covers the days
  that a lower one supplied. The read then takes the higher ranked price for those days.
  A datasource that failed once therefore recovers its rank the next time it is asked
  about the listing.
* Two datasources often answer for one day. A comparison of the two stays left out. The
  read takes one price for each day by rank, and a difference between two sources within
  a spread is expected. Where two sources differ by a split ratio, the corporate actions
  calendar is the place to find it.
* The table also holds the days outside every holding. A listing holds about 6,000 rows from
  Massive, whose history starts in 2003, and 8,000 or more from an EODHD plan that
  serves every year. See
  [010](010-prices-are-a-plain-table-and-the-read-carries-a-close.md).
* A provider serves the history of a symbol, and a reused ticker carries the prices of
  an earlier company. The system stores those prices on every day served, and the chart
  of issue [010](../issues/010-price-chart.md) draws them.
