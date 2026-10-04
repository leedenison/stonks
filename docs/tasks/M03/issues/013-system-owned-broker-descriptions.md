---
title: System owned broker descriptions
type: task
---

## Scope

A mapping, held with system authority, from what one broker calls a line to the
instrument it names, so a stated key carrying only a description resolves through it.

In:

- The system owned broker description: the broker, the description as the broker states
  it, and the instrument or listing it names. It is written from reference data a broker
  publishes, or by an administrator verifying a mapping the stated keys imply. A
  description a user stated is never promoted on its own.
- Resolution matching a stated key's description against the system owned descriptions
  of its broker, after the database's identifiers and before the datasources, as one
  more step of the lookup in the resolve package.
- A finding where the description names an instrument disjoint from the one another
  identifier of the key names.

Out:

- The reference data of any particular broker, and the admin surface for verifying a
  mapping. This issue builds the model and the resolution step; the writers follow.

## Design

A broker description is an identifier type with traits of its own; see
[019](../adr/019-a-broker-description-is-an-identifier.md). A stated key carries its
descriptions among its identifiers, and the resolution step through a system owned
description follows.
