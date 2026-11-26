---
title: Google Sheets tool
type: task
---

## Scope

A command line tool through which an administrator fills gaps in prices and rates from
GOOGLEFINANCE, and the server RPCs it calls. See
[007](../adr/007-the-google-sheets-tool-writes-formulas-and-reads-pasted-values.md).

## Design

Server:

* An RPC returns the gaps that remain for the Google Sheets datasource. Each gap states
  the listing, the open ticker and its venue, and the periods.
* An RPC accepts the prices of a series with their "as at" date and currency code. It
  refuses a series whose code falls outside the family of the listing, and it names the
  series it refused.
* The sign-in accepts an identity token whose audience is a second configured client id.

Tool:

* A write step creates the spreadsheet, or reuses the one it created before, and writes
  the formulas tab: the date cell, and for each series a header, the formulas for each
  candidate exchange code and a `currency` cell.
* A read step reads the values tab and sends each series to the server.
* Both steps report each symbol they skipped or the server refused, with the reason.
* The map from an operating MIC to the exchange codes of Google is a file in the tool.

## Tests

* The Sheets API is replayed from recorded traffic, as the datasource clients are.
* The values tab of a test is a fixture, since the paste is a manual step.

## Facts verified

Each fact was read from a spreadsheet the tool created, on 2026-11-26.

* GOOGLEFINANCE closes are adjusted for splits and are free of any dividend adjustment.
  Google serves 124.81 for AAPL on 2020-08-28, where the stock closed at 499.23 before a
  4 for 1 split.
* A wrong exchange code for a ticker, and an unknown ticker, each show `#N/A` in place
  of the header of the answer.
* Google spells a share class with a dot, as `NYSE:BRK.B`. `BRK-B` and `BRK/B` show
  `#N/A`.
* A `CURRENCY:` pair answers over history under `price` and under `close`. `GBPUSD` is
  the number of USD that one GBP buys. The `currency` attribute fails for a pair, so the
  tool states USD.
* A row carries the close of its session in the local time of the exchange, so its day
  is the day of the session. Hong Kong and Seoul need no shift. Google refuses to serve
  Tokyo to Google Sheets.
* Values pasted from a tab of historical formulas read back through the API. The
  formulas themselves read back as values at some times and as `#N/A` at others.
* The header of an answer is `Date` and `Close`, under either attribute.
* The start date of a formula is inclusive. The end date is inclusive for a security
  and exclusive for a pair.
* Google keeps the time zone `Etc/GMT`, and the date cell follows it.
* A refresh of the OAuth token returns a new identity token.
* Google lists a Cboe BZX security under `BATS`, a Toronto one under `TSE`, and a Hong
  Kong one under its four digit code, as `HKG:0700`.
* London is quoted in pence, and its `currency` cell states `GBX`.
