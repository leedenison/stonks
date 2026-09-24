---
title: Findings and the run admin surface
type: task
---

## Scope

The record a run leaves for an administrator, and the admin pages that read it.

In:

- The finding row: the run that met it, the rows it is about, its kind, and whether an
  administrator has cleared it. A block is reported by a finding that references it.
- The administrator trigger on the run row, alongside user and run.
- Admin RPCs and pages: runs by kind, trigger and state with the items of one run;
  findings listed and cleared; datasources with their enabled state and precedence;
  blocks listed and cleared.
- A telemetry mirror of run counts per kind, trigger and outcome, and of findings per
  kind, bounded and driving nothing.
- An e2e spec reading, as an administrator, the runs and findings the suite's own
  uploads produced.

Out:

- Starting a run from the admin surface; issue [009](009-replay.md).
- The schedule trigger.

## Design

A finding never changes behaviour on its own. A block is the only thing withheld until
an administrator acts, and clearing it clears the finding reporting it. Items are addressed to the user whose work made them, findings to
the administrator.
