---
title: Datasource stub for the e2e suite
type: task
dependencies: [007]
---

## Scope

A service in the e2e overlay that stands in for the provider, so the shipped binary
resolves instruments deterministically without reaching the network.

In:

- A stub service reached through the same configuration that names the real provider,
  serving a corpus keyed on the request. The stub does not know which spec is running:
  one key answers with a listing, another with not found, another with a rate limit. An
  unknown key answers an error naming the key, never a default.
- Fixtures for the cross-broker case: two brokers' exports stating one security under
  different descriptions.
- The compose wiring.

Out:

- Generating the corpus from the integration tier's recordings.

## Design

A spec picks its case by choosing what to ask about, which keeps specs independent and
the suite parallel. A case that is stateful, failing once and then succeeding, needs a
key only one spec uses.
