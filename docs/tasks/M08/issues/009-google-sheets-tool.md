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

## Facts to verify

* That GOOGLEFINANCE closes are adjusted for splits on the ex date and are free of any
  dividend adjustment.
* How GOOGLEFINANCE answers a wrong exchange code for a ticker.
* How Google spells a share class, such as `BRK.B`.
* That a `CURRENCY:` pair answers over history, and its quote direction.
* The date in a row for a listing in Asia, given that Google treats a requested date as
  noon UTC.
* That values pasted from a tab of historical GOOGLEFINANCE formulas read back through
  the API.
