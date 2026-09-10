---
name: adr
description: Architecture decision records for Stonks, in docs/tasks/<label>/adr/ -- what an ADR is, the bar for writing one, the file format and milestone-scoped numbering, and how it is written up into the specification. Use when recording a decision, deciding whether a choice warrants an ADR at all, or writing one up into the spec.
---

# Architecture Decision Records

An ADR records a decision and the reasoning behind it, for one milestone, until it is
written up into code comments.

An ADR can specify state behaviour, constraints, invariants and the reasoning behind them.
ADRs are narrow and cheap to minimize friction for the author.  They assume the reader
has context from the existing system.  An ADR answers for one decision or a small set of
related decisions. ADRs may be narrative, and may refer to the state of the system before
the decision.

## When an ADR is warranted

Write an ADR when a design decision took deliberation, when a principle was extracted,
trade-offs were considered or a non-obvious conclusion was reached.  A decision anyone
would make the same way needs nothing written down at all.  An ADR is deliberately cheap
to write, scoped to one decision, and lives only as long as its milestone. 

An ADR is written when the decision is being made. ADRs should not be written for deferred
milestones. Instead write a note under `docs/deferred/` -- see the `issue-tracker` skill.

## Writing it up

An ADR is transient and is either written up into code comments as its work lands and then
deleted, or discarded as below the threshold of significance at the end of the milestone.
See the `spec` skill for a definition of what should be written up into the code and how
it should be written.

Closed decisions stay recoverable from the git history.

## Naming

`docs/tasks/<label>/adr/NNN-slug.md`. Three digit number, terse kebab-case slug. Numbers
are scoped to the milestone and start at 001. **A number is never reused within its
milestone**. Use the git log over that directory to allocate a new number rather than
`ls`.

House style makes the title a sentence, so the filename states the decision:
`002-the-service-speaks-connect-over-plain-http.md`, not `002-rpc-framework.md`.

An ADR whose milestone is undecided goes in `docs/tasks/orphans/adr/` with a slug and no
number.

## Format

Title as an H1, then the context, the decision and the reason. That is the whole
requirement. An ADR can be a single paragraph, and it carries **no frontmatter**.

```markdown
# Charting is Recharts

The front end needs a charting library. Recharts is used for its composable
component API, its small bundle, and its built-in responsive container, rather
than a wrapper over a lower level library.
```

Two optional sections, added only when they earn their place:

* **Considered options** -- when the rejected alternatives are worth remembering.
* **Consequences** -- when something non-obvious follows downstream.

## Cross-references

Within a milestone, paths are relative to the ADR:

* Another ADR: `[NNN](NNN-slug.md)`
* An issue: `issue [NNN](../issues/NNN-slug.md)`

A bare `003` is ambiguous between the two, so always write the path.

**Nothing outside the milestone refers to an ADR.** It is deleted when the milestone
closes, so code, a deferred note or a later milestone points at the code comments
written from the ADR instead. 

## Compression

* Do not restate another ADR after linking it.
* Fold amendments into the body. An ADR states the decision as it stands, not the sequence
  of revisions that produced it.

## See also

The `spec` skill for what is written up into comments and how it is written, and the
`issue-tracker` skill for milestones and the directories both kinds of file live in.
