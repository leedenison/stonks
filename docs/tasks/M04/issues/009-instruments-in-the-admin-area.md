---
title: Instruments in the admin area
type: task
---

## Scope

Instruments are owned by the system, so the admin area lists every instrument, by name,
a page at a time. The user area has no instruments page and no instrument service.

In:

- An admin instrument service under `stonks.admin.instrument.v1` that lists every
  instrument, each with its identifiers and listings, in name order with page tokens.
  The database names an instrument, as
  `../adr/014-the-database-ranks-identifier-types-for-naming-an-instrument.md` decides.
- An admin page at `/admin/instruments`: a narrow table of name and class on the left,
  and a pane on the right that shows the selected instrument in sections. The selected
  instrument is part of the page's address.
- The pane's identifiers section groups the instrument grain identifiers by type. Its
  listings section shows each listing with its currency and primary venue, and its
  identifiers grouped by type and symbol, with the domains of one symbol on one row.
- The user pages have no instruments page and no navigation entry for one, and
  `stonks.instrument.v1` does not exist.

Out:

- Searching or filtering the instruments.
- Editing an instrument.

## Motivation

The instruments list is a view of system data, not of what a user holds. The holdings
page already names what a user holds. An administrator checking what a resolution
created needs every instrument, not the ones one user's keys reached, and needs the
identifiers readable rather than packed into one row.

## Design

The pane shows the row the list already holds, so there is no RPC that reads one
instrument. The address carries the page token beside the selected id, and an address
whose id is not on its page shows the pane's placeholder. A page token that names an
instrument a merge has since deleted yields an empty page, and the administrator
returns to the first page.
