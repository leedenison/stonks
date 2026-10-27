---
title: Rename corporate events to corporate actions
type: task
dependencies: [013]
---

## Scope

"Events" is the term for the economic events that group transactions. A split or a
spin-off is a corporate action. Code names abbreviate it to "corp act".

| Now | Becomes |
|---|---|
| package `corporate` | `corpact` |
| package `run/refreshcorporateevents` | `run/refreshcorpacts` |
| package `service/admin/refreshcorporateevents` | `service/admin/refreshcorpacts` |
| `CorporateEvent` and its compounds | `CorpAct` |
| tables `corporate_events`, `corporate_event_answers` | `corp_acts`, `corp_act_answers` |
| `FETCH_KIND_CORPORATE_EVENTS` | `FETCH_KIND_CORP_ACTS` |
| proto package `admin/refresh_corporate_events` | `admin/refresh_corp_acts` |
| `RefreshCorporateEventsService` | `RefreshCorpActsService` |

In:

- Go, proto, SQL and the client. The migrations change in place.
- Prose, comments and UI labels say "corporate action" in full.
- The M06 ADRs and issues, including the ADR file names and the links to them.

Identifier events keep their name. Where a name or a sentence could mean either kind,
it says "identifier event" or "corporate action" in full.

The run kind is renamed in [015](015-run-kind-names.md).
