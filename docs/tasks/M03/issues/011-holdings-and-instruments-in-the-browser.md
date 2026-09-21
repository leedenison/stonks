---
title: Holdings and instruments in the browser
type: task
dependencies: [008, 010]
---

## Scope

What a user sees of a resolved holding and of one nothing answered.

In:

- The holdings page showing both kinds: a resolved holding named by its instrument's
  stable identifiers and summed across brokers, and an unresolved one named by the
  identifiers and descriptions its keys state and marked as resting on the user's
  statements alone.
- An instruments RPC and page listing the instruments a user's keys resolved to, with
  their listings and identifiers.
- The statement page distinguishing a key whose identification was unavailable from one
  nothing recognised.
- An e2e spec uploading the two-broker fixtures against the stub and asserting one
  resolved holding.

Out:

- Correcting an identification, and anything else a user records against a key.
- Prices and valuation.
