---
title: Datasources
recorded: 2026-09-14
---

# Datasources

The kinds of data fetched from external providers beyond instrument identity: identifier
events, corporate events, prices and FX rates.

## Why

No provider covers every instrument a user can hold, every provider charges by quota, and
any provider can be down. The framework each fetch goes through is built; what remains is
the data each kind asks for and what it records.

## Model

### Keys

A fetch is made for a key: the primary key of an instrument for prices and corporate
events, and the natural key of an identifier or a domain of one for identifier events.
A fetch keyed on an instrument is sent under one of the instrument's identifiers, a stable
one where the provider accepts it and a MIC-derived one otherwise.

### Coverage

Coverage is owned by the consumer of each kind of data, so each kind records its own
against the key it answers for. A period is covered when the datasource answered for it,
whether or not the answer held any data. A datasource known not to serve a key covers
nothing for it, and a failed fetch covers nothing.

A range answer for identifier events is recorded against the domain rather than against
each value it covers. Coverage of a value is then the union of rows for the value and rows
for its domain.

### Provenance

A price, a corporate event and an identifier event each reference the fetch key that
produced them. The series a provider serves under a ticker is its own history for the
symbol, and no provider promises that history follows the instrument across a
reassignment, so provenance is recorded whichever identifier the fetch was keyed on.

A merge rewrites the instrument on the merged instrument's fetch keys, so provenance
follows the data.

## Constraints

### On-Demand Fetch

No datasource offers bulk fetching over a user's holdings. Data is fetched on demand for
the keys users hold, covering the period they were held. Ingesting a statement is what
demands it, for every instrument and period it newly covers.

### Fetching from Multiple Sources

No datasource covers the complete range of instruments users hold, since each is limited
by geography, asset class, historic period and similar. Each kind of data is fetched from
an ensemble of datasources.

### Partial or Incomplete Data

A datasource may lack coverage for a key or be temporarily unavailable. Each consumer
states how it tolerates a temporary or permanent absence of its data.

## Sketch

Holdings and valuation read a per instrument summary of coverage and validity maintained
at ingest, not the fetch tables. The fetch tables are what the summary is rebuilt from.

## Undecided

- Whether an administrator enabling or disabling a datasource takes effect without a
  restart, which would need the registry re-read and would let two concurrent runs see
  different precedence.

- Whether a fetch stops calling a datasource after some number of consecutive temporary
  failures, rather than spending its retries on every key.

- Whether an administrator-started replay clears the temporary identifier blocks it
  re-tries, so that a provider's bad hour is not cleared by hand.

- Whether a request that fails as a whole, on a failure classified as about the
  identifier, blocks every identifier it carried. A batch of forty keys meeting an outage
  that outlasts the retries leaves forty blocks to clear.
