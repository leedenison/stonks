---
title: Date ticker answers by the trading day of the venue
type: task
status: unreviewed
---

## Scope

A ticker names one instrument on one trading day across the venues of its market. A
resolution joins two answers that carry one ticker in one market and share a UTC
day. It uses the UTC day of each answer as the trading day. See
`sameDayTicker` in [claim.go](../../../../server/internal/resolve/claim.go).

The UTC day and the trading day of a venue differ wherever the venue is far from UTC.
For XNYS, answers at 00:30 UTC and 23:30 UTC on one date fall in two New York days. A
reassignment at the open between them names two instruments, and the resolution still
joins the answers. Answers at 23:30 UTC and 00:30 UTC the next day fall in one New York
evening. The resolution does not join them.

The work covers each place that dates a ticker answer:

* The same-day join between two answers.
* The day on which a resolution asserts an identifier. See `attach` in
  [writer.go](../../../../server/internal/resolve/writer.go).
* The bounds of validity periods. `types.Period` bounds each period at a UTC midnight.

If a change covers one of these places and leaves out the others, the join and the
validity disagree.

## Open questions

* Whether the test is the local date of the venue or the absence of a session boundary
  between the two answers. Two answers on a Saturday and a Sunday have no session
  between them. The second test needs the session calendar of each venue.
* How far the time of an answer stands for the day its data describes. A datasource that
  updates its ticker map in an overnight batch reports the assignment of the day before.
  Two answers on one trading day may then describe two different days.
* Whether any market in the MIC map groups venues in more than one time zone. Each
  venue of such a market has its own trading day.

## Design

Nothing settled.
