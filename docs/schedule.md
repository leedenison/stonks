# Schedule

Label schemes:

* **M** -- functionality.
* **P** -- productionisation.
* **S** -- spike.
* **D** -- deferred.

M, P and S numbers are append-only.

## Scheduled

The milestones that are scheduled, in the order they are implemented. Each has a directory
of open issues and ADRs at `docs/tasks/<label>/`.

- **M01** - Project scaffolding.

```
012 close
```

## Completed

The record of what has been built. A milestone lands here when its issue directory empties.

## Deferred

Each has a note in `docs/deferred` outlining how it might work.

- **D-TXING** - [Transaction ingestion](deferred/transaction-ingestion.md).
- **D-INSTR** - [Instrument resolution](deferred/instrument-resolution.md); collapsing the names sources use onto one instrument.
- **D-PRICES** - [Price ingestion](deferred/price-ingestion.md) from external providers.
- **D-CORP** - [Corporate events](deferred/corporate-events.md); splits and the restatement of recorded quantities.
- **D-IDENT** - [Identifier events](deferred/identifier-events.md); ticker changes and the intervals over which an identifier names one instrument.
- **D-DATASRC** - [Datasources](deferred/datasources.md); the framework every fetch from an external provider goes through.
- **D-DEPLOY** - [Production deployment](deferred/production-deployment.md); TLS, cross-origin access and what each container publishes.

## Spike

A spike exists only on a spike branch, and is listed here when it does.
`docs/tasks/<label>/spike.md` defines its scope. A spike takes precedence over the schedule.
