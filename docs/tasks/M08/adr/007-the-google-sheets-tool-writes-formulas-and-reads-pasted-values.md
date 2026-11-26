# The Google Sheets tool writes formulas and reads values an administrator pasted

Google states that historical GOOGLEFINANCE data cannot be read through the Sheets API or
Apps Script, and that a caller sees `#N/A` in place of each value. In practice the API
returns the formula values at some times and `#N/A` at others. A tool can write the
formulas through the API, but it cannot rely on reading their values.

## The round

* The tool asks the server for the gaps in prices and rates that remain after the other
  datasources.
* It writes one formula for each series into a formulas tab of a spreadsheet it created.
* An administrator opens the spreadsheet, waits for it to evaluate, and pastes the values
  of the whole formulas tab into a values tab.
* The tool reads the values tab through the API and sends the prices to the server.
  Pasted values are plain cells, so the restriction does not apply to them.

## The datasource

Every price references a datasource and a fetch key. Google Sheets therefore has a row
in `datasources`, and its integration lacks every capability, so each run passes over
it.

Each upload records an upload prices run. It is a system run, so the prices outlive the
account of the administrator. A datasource fetch run beneath it holds one fetch key for
each series stored. The fetch key of a security records the symbol Google answered, as
a datasource ticker of Google Sheets. The fetch key of a rate records the currency, as
the other price clients record it. Each fetch key is answered at the start of the day
of the evaluation. A series that the server refuses leaves no fetch key.

## The "as at" date

GOOGLEFINANCE closes are adjusted for splits and are free of any dividend adjustment.
Google does not document the adjustment; it is known from the prices it serves. AAPL
closed at 499.23 on 2020-08-28, the last session before a 4 for 1 split, and Google
serves 124.81 for that day. A split is assumed to be applied on its ex date.

The tool cannot see the day of the evaluation, which falls somewhere between its write
and its read. The formulas tab therefore carries one `=TODAY()` cell. The paste turns it
into a fixed date, and the tool states that date as the "as at" date of every price in
the tab. The tool creates the spreadsheet in the time zone `Etc/GMT`, so the date is the
UTC day and does not depend on the locale of the administrator.

A rate is stored as at its own date. The server applies that rule at the upload, because
a currency never splits, whatever Google serves. See
[001](001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md). The upload
therefore carries the "as at" date once, and the server refuses a date later than the
current UTC day. Coverage ends before the "as at" date, since the close of that day may
be unpublished.

## The ticker

* A formula names the current ticker of the instrument, whatever the dates it asks for,
  because Google serves its history under the symbol as it stands today. The tool uses a
  row of `resolved_tickers` that is still open.
* The map from a venue to the exchange code Google uses lives in the tool, which is the
  client of this datasource.
* One operating MIC can map to several codes: XNYS covers `NYSE`, `NYSEARCA` and
  `NYSEAMERICAN`. The tool writes one formula for each candidate code and takes the
  first that answers, in the order of preference the map gives. Two codes can answer for
  one ticker: XFRA covers `ETR` and `FRA`, and Google lists many securities under both.
  Venues are fungible within a listing, so either close serves. See
  [001](001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).
* A code that lacks the ticker shows `#N/A` in place of the header of the answer.
* Google refuses to serve Tokyo to Google Sheets, so the map leaves it out.
* A `currency` cell beside each series states the currency Google quotes. The tool sends
  it as the currency of the series. See
  [004](004-a-price-is-stored-in-the-family-currency-of-its-listing.md).
* The date of each price comes from the timestamp in its row. Google stamps a row with
  the close of the session in the local time of the exchange, such as 16:00 for New York
  and 15:30 for Seoul, and stamps a rate 23:58. The day of the timestamp is therefore the
  day of the session. A rate has a row for every calendar day.
* Google answers a security through the end date of a formula and a rate only to the
  day before it. A formula gives the day after the last it needs as its end date.

## Coverage

The tool states the coverage of each series, because it is the client of this
datasource. See [011](011-price-coverage-ends-at-the-last-price-served.md).

* Google does not state where its history of a symbol starts or ends. The coverage of a
  gap therefore runs from the first row served to the last.
* A formula asks for a week beyond each end of the gaps of its series. A row inside that
  margin shows that the days without a session at the edge of a gap belong to the
  history, so they count as covered. Without the margin, a gap that starts or ends on a
  weekend would keep those days open for good, since Google Sheets is the last
  datasource.
* Coverage ends before the day of the evaluation, as the server also enforces.
* A listing with several gaps gets one formula across them. The rows between two gaps
  are left out of the upload.

## The spreadsheet

* The rows of the formulas tab touch, from the date cell down. Select all in Google
  Sheets takes the cells that touch the active cell, so an empty row would leave the
  date cell out of the copy.
* Each tab is sized to its round. An administrator copies a whole tab, and a browser
  runs out of memory on a tab of millions of cells, whether or not they hold a value.
* A round holds as many series as fit a budget of cells. The others wait for the next
  round, and the tool reports how many.
* The values tab describes itself. Each series carries its listing, its symbol and its
  gaps beside its rows, so the read step needs only the spreadsheet from the write step.

## What the tool reports

The tool fails loudly, by symbol, for each series it cannot use:

* An instrument that lacks an open ticker or a mapped venue. The gap stays open.
* A values tab that lacks the date cell or still holds a formula.
* A listing that Google answered under none of its candidate symbols, a security whose
  currency Google does not state, and a series whose rows all fall outside its gaps.
* A series that the server refuses, with the reason.

The server does not test corporate actions coverage at the upload. A price from Google
Sheets needs the calendar only from the day of its evaluation. See
[003](003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).

## Sign-in

* The tool signs in to Google as an administrator through an OAuth desktop client on the
  command line. It calls the `SignIn` RPC with the identity token and keeps the session
  it returns. Each RPC it then calls checks the administrator role.
* An identity token from a desktop client carries a different audience from the web
  client, so the server accepts a second configured client id.
* The tool asks for the `drive.file` scope, which reaches only the files the tool
  created.
* The refresh token is cached in the config directory of the user, readable only by its
  owner.
