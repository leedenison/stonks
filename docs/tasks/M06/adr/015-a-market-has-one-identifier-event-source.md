# A market has one identifier event source

Massive is the only datasource that states edges and assertion coverage. It covers only
the US. Ticker validity nonetheless ranks the edges and coverage of every datasource
by precedence, and lets a stronger datasource overrule a weaker one's edge. A provider for
another market would answer only for that market. Two providers never need to compete for
the history of one ticker.

## Decision

`markets.identifier_event_source` names at most one datasource for each market.

- Edges and assertion coverage count only from that datasource. A market without one
  stores the edges and coverage of every datasource and ignores them.
- Assertions from every datasource compete for their days. An identifier attestation
  ranks first, then the identifier event source, then the other datasources by
  precedence.
- Only an identifier attestation overrules an edge. An administrator overrules on purpose,
  so the `overruled_edge` finding kind is removed.
- A ticker history is requested only from the identifier event source of the ticker's
  market. A one-day lookup is requested from every datasource that serves one.
- A change of identifier event source takes effect as
  [016](016-ticker-validity-is-stored.md) states.

The identifier event source is set in seed data. An administrator changes it in SQL and
then starts the rebuild run.

## Consequences

Asking one datasource for ticker histories saves quota and keeps one view of each
market's renames. When the identifier event source misses a rename, only a point that
disagrees with the source reveals it. That point raises `missed_event` inside the source's
coverage.

## Considered options

- Keep precedence across every datasource. This keeps the machinery that overrules edges
  between datasources, and only one datasource uses it.
- Choose the identifier event source by precedence among the datasources that serve a
  market. An administrator could not choose a provider without reordering identity
  resolution too.
