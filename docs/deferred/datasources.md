---
title: Datasources
recorded: 2026-09-10
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
events, the natural key of an identifier for identifier events, and the stated key of a
source for instrument identity.

### Coverage

Coverage is recorded per key, per datasource and per period.  A period is covered when the
datasource answered for it, whether or not the answer held any data.  A datasource known
not to serve a key covers nothing for it, and a failed fetch covers nothing.

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

### Partial or Incomplete Data

A datasource may lack coverage for a key or be temporarily unavailable.  Each consumer
states how it tolerates a temporary or permanent absence of its data.

## Sketch

Nothing settled beyond the model and constraints.

## Undecided

- Whether coverage is one table keyed on the kind of data or one table per kind.

- How a datasource declares what it does not serve, so that a call guaranteed to fail is
  never made.
