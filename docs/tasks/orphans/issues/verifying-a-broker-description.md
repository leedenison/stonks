---
title: Verifying a broker description
type: task
status: unreviewed
---

## Scope

Two sources write system owned broker descriptions: reference data a broker publishes,
and an administrator who verifies a mapping that the stated keys imply. The
administrator works from the admin area. A description a user stated is never promoted
on its own.

A replay re-resolves the keys that a new mapping names. A key stating only a description
is unrecognised, and no replay scope selects it: the datasource scope selects the keys a
datasource serves, and the unavailable scope selects unavailable keys.

## Design

Nothing settled.
