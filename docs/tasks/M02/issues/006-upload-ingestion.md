---
title: Upload ingestion
type: task
dependencies: [003, 004, 005]
---

## Scope

The service side of an upload: accepting the neutral format, validating it, resolving
its instruments against the database and writing the transaction log.

In:

- An RPC accepting an upload and answering with its run.
- Validation of each row in the handler; see
  [../adr/011-four-checks-make-a-row-invalid.md](../adr/011-four-checks-make-a-row-invalid.md).
  An invalid row is rejected on its own and recorded as an item of the run; the
  remaining rows are ingested.
- Replacement of every transaction for the claimed broker whose order date lies in the
  claimed period, so a re-upload is idempotent and an empty range at a boundary is a
  deletion. The deletion, the accepted transactions, the item rows and the run's
  completion are one database transaction; see
  [../adr/007-a-run-is-pending-running-completed-failed-or-interrupted.md](../adr/007-a-run-is-pending-running-completed-failed-or-interrupted.md).
- Storage of one stated key per distinct key in the upload.
- Resolution of each stated key against the database, as a resolution run with the
  upload as parent. A key stating a currency identifier resolves to that listing of the
  cash instrument. Any other key matches an existing user owned listing through its
  broker description identifier, or creates an instrument and listing. A key whose
  description names a listing but states a different currency or asset class
  contradicts it, and its rows are rejected; see
  [../adr/005-a-broker-description-is-a-listing-grain-identifier.md](../adr/005-a-broker-description-is-a-listing-grain-identifier.md).
- A stated split recorded against the run. No split is stored as an event or applied.
- An RPC listing a user's uploads with their outcomes.

Out:

- Datasources. No provider is called, so every instrument is resolved to its broker
  description.
- Replay of a resolution.
- Grouping transactions into events.

## Design

Resolution is keyed on the stated key, so one key is resolved once per upload however
many rows carry it. Every query over uploads, transactions and instruments takes the
caller's user id; see [../adr/001-access-is-scoped-in-the-query.md](../adr/001-access-is-scoped-in-the-query.md).
