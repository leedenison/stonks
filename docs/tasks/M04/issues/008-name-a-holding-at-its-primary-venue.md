---
title: Name a holding at its primary venue
type: task
status: unreviewed
dependencies: []
---

## Scope

Name an instrument holding by the ticker of the listing the user holds, at the
listing's primary venue. Show the venue's common name beside the ticker and the broker's
description beneath it. Show one registry code on the row. A per-row expansion shows the
other registry codes, the listings by currency, and the keys the holding sums with their
statements.

In:

- A listing's primary venue, stated by a datasource, and a table of venues' common
  names with a rank for a listing no datasource has named.
- The holding API carrying each listing named at its venue and the keys the holding
  sums.
- The holdings page row and its expansion.

Out:

- Venue tickers and composite FIGIs on the holdings page. The instruments page is the
  inventory of these.

## Motivation

The holdings page names a holding by whichever venue ticker the API lists first, which
is an arbitrary venue or an OpenFIGI ticker, and lists every composite FIGI beside it.
Nothing on the row says what the user's statement called the holding or which listing
they hold.
