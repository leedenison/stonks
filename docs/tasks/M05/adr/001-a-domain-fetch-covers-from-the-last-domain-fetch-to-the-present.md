# A domain fetch covers from the last domain fetch to the present

A range provider serves identifier events for a domain over a period. The period of each
fetch must be decided.

A domain fetch starts where the most recent domain fetch for the same datasource and
domain ended. Where none exists, it starts on the date of the earliest transaction. It
ends on the present day. Where the provider's history limit falls after the start, the
fetch starts on the earliest date the provider allows.

Each fetch therefore extends coverage to the present without overlap. A gap in coverage
arises only where a provider's history limit forces it.

## Consequences

Extending coverage on a schedule is a later milestone. In this milestone an administrator
starts a domain fetch from the admin area. See issue
[006](../issues/006-manual-identifier-event-fetch.md).
