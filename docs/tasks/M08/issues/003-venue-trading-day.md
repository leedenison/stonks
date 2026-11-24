---
title: Date ticker answers by the trading day of the venue
type: task
---

## Scope

A ticker names one instrument on one trading day across the venues of its market. A
resolution joins two answers that carry one ticker in one market and share a UTC
day. It uses the UTC day of each answer as the trading day. See
`sameDayTicker` in [claim.go](../../../../server/internal/identity/resolve/claim.go).

The UTC day and the trading day of a venue differ wherever the venue is far from UTC.
For XNYS, answers at 00:30 UTC and 23:30 UTC on one date fall in two New York days. A
reassignment at the open between them names two instruments, and the resolution still
joins the answers. Answers at 23:30 UTC and 00:30 UTC the next day fall in one New York
evening. The resolution does not join them.

The work covers each place that dates a ticker answer:

* The same-day join between two answers.
* The day on which a resolution asserts an identifier. See `attach` in
  [writer.go](../../../../server/internal/identity/resolve/writer.go).
* The bounds of ticker validity periods. `types.Period` bounds each period at a UTC
  midnight.

If a change covers one of these places and leaves out the others, the join and the
ticker validity disagree.

## Design

See [009](../adr/009-a-ticker-answer-is-dated-by-the-local-date-of-its-market.md).

* `markets` gains a time zone column, NULL where the seed places none. A market that
  lacks a zone reads as UTC.
* A script in `local/scripts` derives the seed from the ISO 10383 list in `local/ref`, as
  the script for the OpenFIGI exchange codes does. It maps a country that has one zone to
  that zone, and it places the markets of a country that has several by city from a table
  in the script. It prints each market it cannot place.
* One function turns the moment of an answer into the day of its market. The same-day
  join, `attach` and the bounds of a ticker validity period each call it.
* The same function gives the last completed day of a market, which
  [005](005-store-and-read-prices.md) reads.

## Facts to verify

* Every venue of the US market trades one session on New York time.
* Whether the Massive ticker reference endpoint takes a date. If it does, an answer
  describes the day in the request.
