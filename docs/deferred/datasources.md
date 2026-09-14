---
title: Datasources
recorded: 2026-09-14
---

# Datasources

The framework through which instrument identity, identifier events, corporate events,
prices and FX rates are fetched from external providers.

## Why

No provider covers every instrument a user can hold, every provider charges by quota, and
any provider can be down.  Each kind of data fetched meets the same constraints, so they
are recorded once here and each consumer records only what is specific to its data.

## Model

### Keys

A fetch is made for a key: the primary key of an instrument for prices and corporate
events, the natural key of an identifier or a domain of one for identifier events, and
the stated key of a source for instrument identity.

A fetch keyed on an instrument is sent to the provider under one of the instrument's
identifiers.  A stable identifier is sent where the provider accepts one, and the
provider maps it to the ticker it serves.  Otherwise a MIC-derived identifier is sent.

### Coverage

Coverage is recorded per key, per datasource and per period.  A period is covered when the
datasource answered for it, whether or not the answer held any data.  A datasource known
not to serve a key covers nothing for it, and a failed fetch covers nothing.

A range answer for identifier events is recorded against the domain rather than against
each value it covers.  Coverage of a value is then the union of rows for the value and
rows for its domain.  Every coverage row references the run key that earned it.

### Runs

A fetch is a run: one datasource, one kind of data, a set of keys and a period.  A run is
a row, and is the identity a caller holds while the fetch proceeds and reads its outcome
back against.  It records, per key, whether the datasource served it, skipped it as not
served, or failed temporarily or permanently.

A run key also records the identifier the integration sent to the provider, the
instrument the answer was attached to, and every identifier the answer named.  The
identifiers named are the run's assertions.  See
[identifier-events.md](identifier-events.md).

### Provenance

A price, a corporate event, an identifier event and an identifier row each reference the
run key that produced them.  The run key carries the identifier sent and the run carries
the moment it was sent, so every row resting on an identifier that later proves to have
moved is found from the event without searching the data.  The series a provider serves
under a ticker is its own history for the symbol, and no provider promises that history
follows the instrument across a reassignment, so provenance is recorded whichever
identifier the fetch was keyed on.

A merge rewrites the instrument on the absorbed instrument's run keys, so provenance
follows the data.

### Blocks

A permanent failure for a key the integration declared it serves is a block: one row per
key, datasource and kind, carrying the reason.  A blocked key is not fetched from that
datasource again until an administrator clears the block.

### Venues

Each integration holds its own map between the provider's venue codes and operating MICs,
and translates every key it is asked for and every answer it returns.

## Constraints

### On-Demand Fetch

No datasource offers bulk fetching.  Data is fetched on demand for the keys users hold,
covering the period they were held.  The datamodel records which periods have been fetched
successfully for each key and datasource, and whether a fetch failed temporarily or
permanently.

### Fetching from Multiple Sources

No datasource covers the complete range of instruments users hold, since each is limited
by geography, asset class, historic period and similar.  Each kind of data is fetched from
an ensemble of datasources.

### Minimizing Fetch Cost

Fetching is expensive because of API quotas and rate limits.  Duplicate fetches when more
than one datasource covers a key are avoided, as are calls guaranteed to fail because the
datasource is known not to serve the key requested.

### Pluggable Datasources

More datasources are added as the system evolves.  The code integrating a datasource
conforms to a well defined interface, with the option to extract an integration into a
separately maintained library.

Any running instance may have access to a different combination of datasources, so an
integration can be enabled or disabled.

### Not Served is Known Before Calling

An integration answers locally, without spending quota, whether it serves a key.  A key it
does not serve is skipped, counted in the run for the admin view, and gains neither
coverage nor a block.  A failure on a call for a key the integration declared it serves is
classified by the integration as temporary or permanent, since only it knows the
provider's codes.

### Partial or Incomplete Data

A datasource may lack coverage for a key or be temporarily unavailable.  Each consumer
states how it tolerates a temporary or permanent absence of its data.

## Sketch

The framework owns the registry of integrations with each one's enabled state and
precedence, the run and coverage rows, blocks and their clearing, a rate limiter per
datasource, the admin surface and a bounded telemetry mirror of the rows.  Each
integration owns its request shapes, its parsing, the storage of its answers, its venue
map, its symbol spelling map and the declaration of what it serves.

Holdings and valuation read a per instrument summary of coverage and validity maintained
at ingest, not the run tables.  The run tables are what the summary is rebuilt from.

The admin surface lists datasources with their status, starts a fetch and answers with
the run, lists runs, and lists and clears blocks.  Telemetry mirrors counts per
datasource, kind and outcome; the admin view reads the rows.

## Undecided

- Whether coverage is one table keyed on the kind of data or one table per kind.

- How long a temporary failure suppresses the next call, and where that interval is held.
