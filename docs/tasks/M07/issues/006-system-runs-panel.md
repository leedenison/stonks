---
title: System runs panel
type: task
dependencies: [005]
---

## Scope

The system runs become a Runs card on the alerts page. See
[005](../adr/005-the-action-bar-holds-the-page-s-own-actions.md).

* The card sits in a side column at wide widths and stacks below the alerts at narrow
  widths.
* One row per system run: its title, its description, the last run of that kind as a
  state chip linking to the run, and a Start button that opens `StartRunDialog`. The
  last run comes from `ListRuns` with the kind filter, the administrator trigger and a
  page size of one. Debug runs sit under a subheading.
* The runs page action bar carries only the page's own actions. `SystemRunMenu` goes.
  `systemRuns` and `StartRunDialog` move out of `client/app/admin/runs/` with their unit
  tests, and the file comments of `runs/page.tsx` and `start-run-dialog.tsx` are
  rewritten for what each file does.

## Tests

* The unit tests of the runs page and the dialog move with the code and cover the card,
  the last-run chip and starting a run from it.
* The e2e specs that start system runs do so through the RPC and are unchanged.
