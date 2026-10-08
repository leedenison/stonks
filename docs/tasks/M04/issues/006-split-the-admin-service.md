---
title: Split the admin service
type: task
status: unreviewed
dependencies: [004, 007]
---

## Scope

One proto service and one Go package serve every administrator RPC. They cover runs and
their findings, datasources, datasource blocks and replays. Split them into one service
per area, each with its own proto service, handler package, tests and mock.

In:

- The areas, and the RPCs each serves.
- Moving each area's messages, RPCs, handlers and tests.
- The client's typed clients and hooks for each new service.

Out:

- Any change to what an RPC does.

## Motivation

The service has ten RPCs across four areas. Its handler, its tests and its mock are each
around 700 lines long.
[004](004-confirming-a-candidate.md) adds to it.
