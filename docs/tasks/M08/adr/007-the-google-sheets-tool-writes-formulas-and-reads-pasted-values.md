# The Google Sheets tool writes formulas and reads values an administrator pasted

Google states that historical GOOGLEFINANCE data cannot be read through the Sheets API or
Apps Script. A caller sees `#N/A` in place of each value. A tool can write the formulas
through the API, but it cannot read their values.

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
in `datasources`, and its integration lacks the price capability, so a fetch run passes
over it. Each upload records a run and a fetch key of its own.

## The "as at" date

GOOGLEFINANCE closes are adjusted for splits. Google does not document the adjustment; it
is known from the prices it serves. A split is assumed to be applied on its ex date.

The tool cannot see the day of the evaluation, which falls somewhere between its write
and its read. The formulas tab therefore carries one `=TODAY()` cell. The paste turns it
into a fixed date, and the tool states that date as the "as at" date of every price in
the tab. The tool sets the time zone of the spreadsheet when it creates it, so the date
does not depend on the locale of the administrator. A rate is stated as at its own date.

## The ticker

* A formula names the current ticker of the instrument, whatever the dates it asks for,
  because Google serves its history under the symbol as it stands today. The tool uses a
  row of `resolved_tickers` that is still open.
* The map from a venue to the exchange code Google uses lives in the tool, which is the
  client of this datasource.
* One operating MIC can map to several codes: XNYS covers `NYSE`, `NYSEARCA` and
  `NYSEAMERICAN`. The tool writes one formula for each candidate code and takes the one
  that answers.
* A `currency` cell beside each series states the currency Google quotes. The tool sends
  it as the currency of the series. See
  [004](004-a-price-is-stored-in-the-family-currency-of-its-listing.md).
* The date of each price comes from the timestamp in its row, because Google treats a
  requested date as noon UTC.

## What the tool reports

The tool fails loudly, by symbol, for each series it cannot use:

* An instrument that lacks an open ticker or a mapped venue. The gap stays open.
* A values tab that lacks the date cell, or in which the cell still holds a formula.
* A series that the server refuses for its currency.

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
