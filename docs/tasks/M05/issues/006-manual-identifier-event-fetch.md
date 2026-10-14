---
title: An administrator starts an identifier event fetch
type: task
dependencies: [002]
---

## Scope

An administrator starts either fetch from the admin area, beside the runs it creates:

- a domain fetch, over the period that
  [001](../adr/001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md)
  states;
- a re-assertion of every held value, which extends each bracket to the present. See
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md).

Each fetch runs as a system run. The run shows its events and the coverage it wrote.

The datasources page shows the lag of each datasource and lets an administrator change
the lag.
