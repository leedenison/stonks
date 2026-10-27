---
title: Admin alerts
recorded: 2026-10-26
---

# Admin alerts

One page in the admin area that lists what needs an administrator's attention, across
every surface, ordered by how much rests on each item.

## Why

Each surface keeps its own list. The runs page shows the findings of one run at a time.
The blocks page lists blocks. The gaps in a ticker's validity are read per ticker. An
administrator who wants to know what to do next must visit each in turn, and nothing
ranks one surface's items against another's.

A ticker's validity is read under a tickers tab on the instruments page, starting from
the instrument the ticker names. A ticker that names nothing on any day, because every
datasource answers that it does not know the ticker, has no instrument, so the tab
cannot reach it. The alerts page is the one place such a ticker appears.

## Sketch

An alert is derived when read, as ticker validity is. Each kind is a query over rows that
already exist. An alert disappears when an administrator settles its cause.

Each alert links to the surface that settles it: the attest dialog, the run, the block,
the instrument.

Candidate kinds:

* A ticker validity gap. Part of a ticker's transaction period lies outside every
  confirmed interval. Ordered by affected transactions, then affected keys. Settled by an
  identifier attestation, or by a datasource with a longer window.
* An unresolved ticker. Keys with transactions state the ticker, and it names nothing on
  every day. Settled by an identifier attestation that names an instrument, or by a
  datasource that learns the ticker.
* A run finding. The findings of the kinds dropped, contradiction, merged, identifier
  disagreement and broken ticker validity bracket. Each records a choice the system made
  between sources that an administrator may want to check.
* A block. A call failed unrecoverably and the datasource is not asked again under that
  key or at all until an administrator clears the block.
* A failed or interrupted run.
* An unhandled corporate action. One that the system stores without applying it, reported
  by a finding of the fetch that met it; see
  [corporate-actions.md](corporate-actions.md). A split a statement states that the
  calendar lacks is reported the same way.
* A broker description awaiting verification. Stated keys imply a mapping from a broker's
  description to an instrument that an administrator has yet to confirm.

## Undecided

* Whether an administrator can dismiss an alert, or whether it clears only when its cause
  is settled.
* Whether every finding is an alert, or only the kinds that need a decision. A dropped
  answer is routine on most runs.
* How the kinds rank against one another in one list.
* Whether the per-surface lists stay once the alerts page exists.
