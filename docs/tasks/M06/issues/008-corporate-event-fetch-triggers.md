---
title: Corporate event fetch triggers
type: task
dependencies: [003, 004]
---

## Scope

What starts a corporate event fetch, and what it asks for.

In:

- A statement run ends with a child fetch run.
- A run that writes new identifier facts ends with a child fetch run, because attribution
  may move to a ticker that has never been fetched.
- An administrator starts a fetch of every held ticker from the runs page, as with
  Extend validity. The fetch is the child of a run of a new kind, which the `run_kind`
  comment in `002_runs.sql` describes. It skips the cache, as Extend validity does.
- A fetch asks for each ticker that validity says named a held instrument. A dated key
  reaches its instrument through validity, and a fixed key names it. The fetch asks for
  the whole period, from the earliest as-at date of the instrument's transactions to
  past today.
- A fetch for the tickers of other users runs beneath a system run, as replays do.

Out:

- Scheduled fetches and the lookahead. Holdings never start a fetch.
