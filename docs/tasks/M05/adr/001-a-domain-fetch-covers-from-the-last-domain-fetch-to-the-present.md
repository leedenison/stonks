# A domain fetch covers from the last domain fetch to the present

A range provider serves identifier events for a domain over a period. The period of each
fetch must be decided.

A domain fetch ends on the present day. It starts at the latest of three dates:

- the end of the most recent domain fetch for the same datasource and domain, less the
  datasource's lag, or the date of the earliest transaction where no such fetch exists;
- the first date for which the endpoint holds data;
- the earliest date the subscription's history window allows.

The lag is a setting of each datasource. A provider may publish an event some time after
it takes effect, so each fetch repeats the most recent part of the previous period. An
event that arrives late is still recorded, and it unwinds what rested on the earlier
claim that no event occurred.

Ending coverage a lag before the present was rejected. It would leave every recent
transaction outside coverage.

## Consequences

A gap in coverage arises only before the endpoint's first date or the history window.
See [005](005-validity-is-bootstrapped-from-the-present-backwards.md).

Extending coverage on a schedule is a later milestone. In this milestone an administrator
starts a domain fetch from the admin area. See issue
[006](../issues/006-manual-identifier-event-fetch.md).
