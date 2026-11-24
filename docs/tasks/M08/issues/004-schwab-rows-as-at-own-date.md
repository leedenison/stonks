---
title: State each Schwab row as at its own date
type: task
---

## Scope

The Schwab marshaller assumes that a Schwab transaction history states each quantity as
traded. It states each row as at the date of its own line. A split after a trade then
restates the holding through the corporate actions calendar.

The assumption is unconfirmed. See
[confirm-schwab-as-at-basis.md](../../orphans/issues/confirm-schwab-as-at-basis.md).

The work covers:

* The marshaller, its file comment and its tests in
  [schwab.ts](../../../../client/lib/marshal/schwab.ts).
* The export date that the upload dialog requests. Schwab is the only format that needs it.
* The stated split check, which compares a split line with the calendar.

## Design

Nothing settled.
