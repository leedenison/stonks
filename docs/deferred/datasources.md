---
title: Datasources
recorded: 2026-09-14
---

# Datasources

The kinds of data fetched from external providers beyond instrument identity: identifier
events, corporate actions, prices and FX rates.

## Why

No provider covers every instrument a user can hold, every provider charges by quota, and
any provider can be down. This note covers the data each kind asks for and what it
records.

## Model

### Keys

A fetch of prices is made for the primary key of an instrument.
It is sent under one of the instrument's
identifiers, a stable one where the provider accepts it and a MIC-derived one otherwise.

### Coverage

Coverage is owned by the consumer of each kind of data, so each kind records its own
against the key for which it answers. A period is covered when the datasource answered
for it, whether or not the answer held any data. Where a datasource is known not to serve
a key, it covers nothing for that key, and a failed fetch covers nothing.

### Provenance

A price and a corporate action each reference the fetch key that produced them. The
series a provider serves under a ticker is its own history for the symbol, and no
provider promises that history follows the instrument across a reassignment, so
provenance is recorded whichever identifier keyed the fetch.

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

A per instrument summary of corporate action coverage, maintained at ingest and rebuilt
from the fetch tables, would serve holdings and valuation once reading coverage from the
fact tables costs too much.

## Undecided

- Whether a fetch stops calling a datasource after some number of consecutive temporary
  failures, rather than spending its retries on every key.
