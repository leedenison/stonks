---
title: Editing datasources in the admin area
type: task
---

## Scope

An administrator enables or disables a datasource, sets its endpoint and credential,
and orders the datasources by precedence, from the datasources page, with the change
taking effect at once.

In:

- The datasources page as an editor: each datasource a row with its precedence, state,
  endpoint and whether a credential is held, a toggle for its state, an edit dialog for
  the endpoint and credential, and a drag handle that reorders the rows, the first row
  consulted first.
- The RPCs the page calls, refusing a datasource without an integration in this build
  and an order that does not name every datasource once.
- The registry reloading from the table when a datasource changes, so no restart is
  needed, each resolution reading the enabled datasources once as it starts.

Out:

- Registering a datasource. A row is created by the seed or by hand.
- Reaching other processes. One process holds one registry.
