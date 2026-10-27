---
title: Broker descriptions in the admin area
type: task
---

## Scope

Two sources write system owned broker descriptions. One is reference data a broker
publishes. The other is an administrator who verifies a mapping that the stated keys
imply. A description a user stated is never promoted on its own.

An association is provisional when a key resolves through a broker description. A
broker may rename a line without any source reporting it. The rename is an identifier
event. An administrator confirms one association. An administrator may also confirm, in
a single step, the associations of every key that resolved through one description.

A replay re-resolves the keys that a new mapping names. A key that states only a
description is unrecognised, and no replay scope selects it. The datasource scope selects
the keys a datasource serves, and the unavailable scope selects unavailable keys.

## Design

Nothing settled. An association through a broker description is fixed and provisional.
See the `stated_keys` comment in
[004_stmts.sql](../../../../server/internal/db/migrations/004_stmts.sql).
