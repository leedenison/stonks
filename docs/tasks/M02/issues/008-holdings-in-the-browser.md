---
title: Holdings in the browser
type: task
---

## Scope

- A holdings query: the raw quantity of each instrument a user holds, derived from
  their transaction log, including cash.
- An RPC serving it, scoped to the caller.
- A holdings page.
- An e2e spec that uploads a fixture and asserts the holdings it produces.

Out:

- Split adjusted quantities, prices and valuation.
- Portfolios other than the degenerate one holding everything.

## Design

A holding is the sum of a user's transaction quantities in one instrument. Quantities
are shown raw, since no corporate event coverage exists to adjust them.
