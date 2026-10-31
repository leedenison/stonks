---
title: Alerts page
type: task
dependencies: [003, 004]
---

## Scope

The alerts page is the admin home. See
[004](../adr/004-the-alerts-page-is-the-admin-home-and-the-per-surface-lists-stay.md).

* `/admin` renders the alerts page. The body of `client/app/admin/page.tsx` and the
  first entry of `admin-nav.tsx` are rewritten; the entry is Alerts and shows the open
  count from the digest.
* A tile per kind with its count. The tiles are the filter: one narrows the table to its
  kind, and the open tile returns to the digest.
* The digest: one table grouped by kind in rank order, each group with a one-line
  explanation of why the kind needs an administrator, and a closing row that opens the
  kind's full list where the count exceeds the top five.
* A kind's full list, paged with `Pager`.
* One action per row. Each action opens the surface that settles the alert. Open run
  links to the run.
  Clear on a block calls `ClearBlock`. Attest on a ticker that names nothing or a gap
  opens `AttestIdentifierDialog`, which takes a market ticker and so serves a ticker
  with no instrument. Decide opens the dialog of
  [003](003-decide-corp-act-restates-shares.md). Clear on a finding calls
  `ClearFinding`.
* A section for broker descriptions awaiting verification, labelled as a mock, with
  static rows that are display only. It stands for the orphan
  [broker-descriptions-in-the-admin-area.md](../../orphans/issues/broker-descriptions-in-the-admin-area.md)
  so the page's shape accounts for it.
* The mock in `local/ui-mocks/alerts.html` is the reference for the layout.

## Tests

* Unit tests of the page cover the digest, the filter, the paged list and each row
  action.
* `e2e/tests/shell.spec.ts` asserts the admin home is the alerts page.
* An e2e spec reaches the page through kinds the suite already produces: a block from
  `ibkr-blocked.qfx`, an unmatched split from `standard-ko.json` and a gap from the
  identifier attestation fixture, and settles one of each through its row action. A
  failed run is proved in the integration tier of [004](004-alert-reads.md); if its row
  must be seen rendered, a `seedRun` helper beside `deleteRuns` in `e2e/helpers/db.ts`
  seeds it.
