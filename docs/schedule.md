# Schedule

Label schemes:

* **M** -- functionality.
* **P** -- productionisation.
* **S** -- spike.
* **D** -- deferred.

M, P and S numbers are append-only.

## Status

The project is pre-release, so nothing it has built is depended on from outside it. A
schema change edits the migration that defines the object rather than adding one, and a
database that has already applied that migration is recreated: the dev volume through
`make clean-docker`, while the test and e2e databases are tmpfs and start empty on every
run.

## Scheduled

The milestones that are scheduled, in the order they are implemented. Each has a directory
of open issues and ADRs at `docs/tasks/<label>/`.

- **M04** - Identity datasources for Massive and EODHD, and confirming a candidate that a
  fetch served for a key it could not associate.

```
  002 massive
      |
  003 eodhd   004 confirm
        \      /
     006 split admin
           |
       005 close
```

## Completed

The record of what has been built. A milestone lands here when its issue directory empties.

- **M01** - Project scaffolding.
- **M02** - Transaction ingestion from three brokers' exports, and the holdings derived
  from it.
- **M03** - System owned instruments, the datasource framework, and resolution of stated
  keys against one identity datasource.

## Deferred

Each has a note in `docs/deferred` outlining how it might work.

- **D-EVENTS** - [Events](deferred/events.md); grouping transactions into the economic events whose legs they are.
- **D-INSTR** - [Instrument resolution](deferred/instrument-resolution.md); the life of a listing, options, validity, and the replays that keep an association current.
- **D-GUESS** - [Guesses](deferred/guesses.md); the sources that rank candidates where the stated data leaves an association open, and the arbiter an association records.
- **D-PRICES** - [Price ingestion](deferred/price-ingestion.md) from external providers.
- **D-CORP** - [Corporate events](deferred/corporate-events.md); splits and the restatement of recorded quantities.
- **D-IDENT** - [Identifier events](deferred/identifier-events.md); ticker changes and the intervals over which an identifier names one instrument.
- **D-DATASRC** - [Datasources](deferred/datasources.md); identifier events, corporate events, prices and FX rates.
- **D-RUNS** - [Runs](deferred/runs.md); replays that an event or new coverage causes, the schedule trigger, and running on more than one process.
- **D-COMPANY** - [Companies](deferred/companies.md); issuers identified by CIK and LEI, and the instruments they issue.
- **D-ANNOT** - [Annotations](deferred/annotations.md); what a user records against their own keys: pins, groupings, prices and keys made by hand.
- **D-DEPLOY** - [Production deployment](deferred/production-deployment.md); TLS, cross-origin access and what each container publishes.

## Spike

A spike exists only on a spike branch, and is listed here when it does.
`docs/tasks/<label>/spike.md` defines its scope. A spike takes precedence over the schedule.
