---
name: issue-tracker
description: The Stonks task tracker in docs/tasks/, the schedule in docs/schedule.md, and the deferred notes in docs/deferred/. Covers the milestone directories and the issues and ADRs inside them, file naming and milestone-scoped numbering, the issue frontmatter schema (title, type, status, dependencies), tasks versus bugs versus orphans, unreviewed issues and the review that clears them, the start and end issue every milestone has, the M/P/S/D-NAME labels, how to open, close and depend an issue, and how to defer a feature so it is not mistaken for current work. Use when opening, closing, reviewing or depending an issue, when adding or closing a milestone, or when something is wanted eventually but is not being built now.
---

# Task Tracker

## Layout

Within the tasks directory:

```
  tasks/
    M01/
      issues/      the open issues of a scheduled milestone
      adr/         the design decisions taken for it
    M02/
    orphans/
      issues/      tasks that are not yet assigned to a scheduled milestone
      adr/         design decisions that are not yet assigned to a scheduled milestone
    bugs/          bugs that are not yet assigned to a scheduled milestone
```

**A file's directory is its milestone assignment.** A milestone directory is created when
the milestone is scheduled and disappears when its last file is deleted.

Several milestone directories exist at once. Work that is discovered during M01 and clearly
belongs to M02 goes straight into `M02/issues/`; only work whose milestone has not been
decided is an orphan.

The `adr/` directory holds the milestone's decisions. See the `adr` skill for how they
are written.

## Types

Every issue is a **task** or a **bug**.

A **bug** is the system not doing what the specification says, or would say if the
specification were written in enough detail.  Defects in code the current milestone is
still writing are not bugs and should not be filed.  They should be fixed in the
milestone. Bug tracking starts when issues are found in code from previous milestones.

A **task** in `orphans/` is work that has not yet been aggregated into a milestone with
related work.  Orphan tasks are pulled into milestones during scheduling review after 
one milestone is feature complete but before work on the next has been started.

## Granularity

A **task** is coarse grained and feature level, for example "Portfolio holdings in the
browser". Do not create fine grained implementation tasks; those belong in the detailed
sections within a task.

A **bug** is as fine grained as the defect actually is.

An **orphan** may be a thin placeholder. Its scope is precisely what has not been decided,
so requiring it to be fully formed would stop it being written down at all.

## Naming

`docs/tasks/<label>/issues/NNN-slug.md`. Three digit number, terse kebab-case slug derived
from the title.

**Numbers are scoped to the milestone** and start at 001. A number is never reused within
its milestone, counting the numbers used by issues that have closed and are no longer in
the tree; read those from the git log over that directory, not from `ls`. Rescheduling an
issue into another milestone gives it a new number there.

**Orphans and bugs are not numbered.** They are `<slug>.md`, and an orphan is numbered when
it moves into a milestone. Nothing holds a stable reference to an orphan, which is the
price of being able to write one down without deciding anything first.

## Frontmatter

```markdown
---
title: Portfolio holdings in the browser
type: task
status: unreviewed
dependencies: [005, 006]
---

The round trip: sign in with Google, call the API over protobuf with the session, and
render holdings derived from the transaction log.
```

| Field | Required | Notes |
|---|---|---|
| `title` | yes | single line; the slug is derived from it |
| `type` | yes | `task` or `bug` |
| `status` | no | `unreviewed`, and nothing else; absent means reviewed |
| `dependencies` | no | issue numbers within the same milestone |

Ordering between milestones is the schedule's job, so a dependency never crosses one.

Do not restate the frontmatter in the body.

## Review

`status: unreviewed` means the issue was written quickly, to get it recorded without
stopping to think it through. It exists so that noticing something during development costs
one file and no decisions.

An issue with the field absent has been reviewed, and that asserts four things:

* It is **coherent** -- it says one thing, and that thing can be acted on.
* It is **needed** -- the work is actually wanted.
* It closes open questions and defines chosen solutions where needed.
* It is the right **type**, the right **granularity**, in the right **directory**, with the
  right **dependencies**.

## Body

Free-form markdown. Conventional sections, used when they have something to say:

* `## Scope` -- what is in and what is not.
* `## Motivation` -- why, when it is not obvious.
* `## Design` -- the shape of the intended solution.

Cite an ADR in the same milestone as `../adr/NNN-slug.md`, and another issue by its path.

### Transcribed code

An issue may carry verbatim code or schema when the shape was settled as the issue was
filed, settling it took real deliberation, and writing it down stops that work being done
twice. This is the exception rather than the rule.

## Workflow

* **Open**: allocate the next number in the milestone and add the file to the directory
  that says where it belongs. Mark it `status: unreviewed` unless it is being written with
  the care that review asserts.
* **Close**: **delete the file, in the commit that closes it.** An issue is never marked
  closed and left in the tree.
* **Reopen**: re-create the file under its original number.
* **Reschedule**: `git mv` it to another milestone, renumbering it there.
* **Depend**: append the number to `dependencies`.

### Closing deletes the file

Every issue in `docs/tasks/` is open. `status` is a property of an open issue.  This
keeps the tracker a worklist rather than an archive.  A closed issue is not lost. The
commit that deletes the issue is the commit that closes it.

* Say what was done in the **commit message**, not in a `## Resolution` section nobody will
  read, since the file is gone in the same commit.
* `git log --follow -- docs/tasks/**/NNN-slug.md` recovers the whole history of any issue,
  across the directories it moved between.
* `git log --diff-filter=D -- docs/tasks/` lists everything ever closed.

## Milestones

`docs/schedule.md` holds the **schedule**: the scheduled milestones in the order they are
implemented. That order is maintained by hand, because it is a decision and not a
consequence of the numbering.

Under the current milestone it also carries a **graph of that milestone's open issues**,
drawn from their `dependencies`. It is an illustration, the dependencies frontmatter is
the source of truth.

The **current milestone** is the spike, if the current branch has one, and otherwise the
first entry in `## Scheduled`. **A milestone closes when both its `issues/` and its `adr/`
directories are empty**: every issue implemented, and every decision either written up into
the specification or discarded.

**M**, **P** and **S** numbers are append-only and never reused.

Every milestone has a **start issue** and an **end issue**. They are numbered like any
other issue and carry no dependencies: the start issue is first by definition and the end
issue is last.  **Both are checklists.**

Milestones tend to be populated with tasks either top down, bottom up or some combination
of the two:

- **Top down.** The milestone is decided first and then populated. Open questions are 
  pre-emptively generated and populated in the start issue.
- **Bottom up.** Issues accumulated in `orphans/` and are aggregated into a milestone
  during review. The open questions are already recorded in their respective issues.

### The start issue

Titled `Open <label>`. Its job is to settle what the milestone is and to schedule the work
in it: the issues it contains, and the dependency edges between them.

```markdown
- Settle any open questions in this file or open issues.
- Record what those questions settle as ADRs in this milestone's `adr/`.
- Create any new issues needed to complete this milestone.
```

### The end issue

Titled `Close <label>`. Its job is review, acceptance and handover, and it is the last issue
of the milestone to close.

```markdown
- Review the codebase for quality issues.
- Run the acceptance pass.
- Write up or discard remaining milestone ADRs.
- Review any issues marked `status: unreviewed`.
- Review docs/tasks/orphans/ and docs/tasks/bugs/ for scheduling.
- Set up the next milestone or spike.
```

What each of those means:

* **Read the milestone's code** for what accumulates across a milestone rather than within
  any one issue of it: duplicate type definitions, near-identical helpers, dead code, drift
  between the specification and what was built.
* **The acceptance pass** is user testing which will be carried out manually.
* **Empty the `adr/` directory**: anything not already written up goes into code
  comments now, or is deleted as not worth keeping. The `adr` skill describes how.
* **Review the unreviewed issues** tree-wide, as described above. Each is confirmed,
  corrected, moved, deleted or punted.
* **Sweep `orphans/` and `bugs/`**: each entry is pulled into a milestone, left where it
  is, or deleted.
* **Setting up the next milestone or spike** creates its directory with a start issue and
  an end issue (if they don't already exist), and for a spike the branch and its document.

A quality problem *created* during the milestone is fixed in the end issue. A quality
problem merely *discovered* during it may become an issue for a later milestone instead.

## Deferred

A deferred entry goes under `## Deferred` in `docs/schedule.md` with a note at
`docs/deferred/<slug>.md`. The two always correspond.

**A deferred entry carries a name, not a number** -- `D-TXING`, not `D01`. A number would
put it in a sequence alongside the schedule.

* **Delete on promotion.** Scheduling a deferred feature moves it into the schedule with an
  M number and creates its directory. Its note becomes the raw material for the milestone's
  start issue (the open questions it records are the ones that issue has to settle). Once
  the work lands and its decisions are written up, the note and its deferred entry are
  removed.
* **Nothing refers to a note.** The index in `docs/schedule.md` is the only place that links
  to `docs/deferred/`, and the only place a deferred entry appears. 

## See also

The `adr` skill, for the decisions a milestone records and how they are written up, and the
`spec` skill, for what comments in the code, a deferred note and a spike document each
contain.
