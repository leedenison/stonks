---
title: Name each run kind with a verb and a noun
type: task
dependencies: [014]
---

## Scope

Each run kind names what the run does to what.

| Now | Becomes |
|---|---|
| `statement` | `ingest_stmt` |
| `resolution` | `resolve_stmt_keys` |
| `fetch` | `datasource_fetch` |
| `replay` | `replay_run` |
| `attestation` | `attest_identifier` |
| `extend_validity` | `reassert_tickers` |
| `refresh_corporate_events` | `refresh_corp_acts` |

A package or a service that exists to start one kind of run takes the name of that kind.

| Now | Becomes |
|---|---|
| package `run/replay` | `run/replayrun` |
| package `run/extendvalidity` | `run/reasserttickers` |
| package `identity/attest` | `identity/attestidentifier` |
| package `service/admin/replay` | `service/admin/replayrun` |
| package `service/admin/extendvalidity` | `service/admin/reasserttickers` |
| `ReplayService`, `StartReplay` | `ReplayRunService`, `StartReplayRun` |
| `ExtendValidityService`, `StartExtendValidity` | `ReassertTickersService`, `StartReassertTickers` |
| `RefreshCorpActsService`, `StartRefreshCorpActs` | unchanged |
| `Attest`, `WithdrawAttestation` | `AttestIdentifier`, `WithdrawIdentifierAttestation` |

`statement`, `resolve` and `market` start more than one kind of run, and keep their
names.

In:

- The `run_kind` enum in SQL, `RunKind` in `run.proto`, the Go constants and the client
  labels.
- The proto packages, the Connect services, the client hooks and the dialogs that start
  each run.
