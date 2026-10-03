---
name: spec
description: The package and files level comments that describe system behaviour, deferred notes in docs/deferred/, and the spike document in docs/tasks/<label>/spike.md. Covers what each claims, how much detail belongs at each level, and what subjects to cover. Use when writing or updating any of these documents, after a change alters system behaviour, or when deciding whether something belongs package level comments, an ADR or a deferred note.
---

# Specification Documents

Several kinds of document describe behaviour. They differ in what they claim.

| Location | Claims |
|---|---|
| Package or file level comments in code | specification of the existing system; the `comments` skill says how they are written |
| `docs/deferred/` | future unscheduled ideas for functionality |
| `docs/tasks/<label>/spike.md` | this is what the spike is exploring |

All are present tense. The directory carries the tense, so a reader who lands in
`docs/deferred/` cannot mistake it for current behaviour.

## Terminology -- `docs/deferred/terminology.md`

One entry per term: what the word means, and nothing about what the system does with it.
Terms are defined here once for use in deferred notes.  Once code is written these
definitions are moved to comments on their primary artifact (eg. often sql datamodel
or a protobuf message type).  Do **NOT** repeat the definition on derived types (eg.
do not repeat the definition on database row types, etc).

## Specifications -- Package and file level comments

Present tense, describing the system as it has been decided and built in terms of
system behaviour, invariants maintained, constraints adhered to and conventions followed.

How a package or file comment is written, and what belongs at that level rather than on
a declaration, is in the `comments` skill; load it with this one.

### Writing up an ADR

A milestone's ADRs move into package or file level comments as their work lands. Writing
up an ADR is  a rewrite, not a copy:

1. **Take only what is significant** -- ie. hard to reverse, surprising without context,
   the result of a real trade-off, a central concept with broad implications. The rest is
   left in the ADR in the git history.
2. **Strip the narrative**, and anything stated in reference to how the system was before
   the decision. A specification describes the system as it is.
3. **Place it**: the most specific package that contains the concept, or if necessary
   (eg. for a component wide convention) the top level package or file.
4. **Delete the ADR** in the same commit.

Nothing in code comments refers to an ADR. An ADR is deleted when its milestone closes.

After content has been added to code comments by an ADR, review it for consistency with
the existing package and file level comments in the edited and related packages to ensure
that it accounts for other requirements where they interact.  Any inconsistencies or
requirements unaccounted for must be raised immediately.

## Deferred -- `docs/deferred/`

Present tense, describing functionality we intend to support but have not built. These are
**not full specifications**. They are notes on the important elements, to be fleshed out
when the work is scheduled.

One file per capability, named for the capability:

```markdown
---
title: <the capability, as a noun phrase>
recorded: <the date the thinking happened>
---

# <title>

One sentence saying what the feature is.

## Why       -- the problem it solves, and why the absence hurts
## Sketch    -- how it might work; say "nothing settled" when that is the truth
## Undecided -- the open questions, which are the most useful part
```

* **Date it.** `recorded` is when the thinking happened. Nothing maintains a note, and a
  reader needs to know how far the system has moved since.
* **Say what is undecided.** Open questions are an important part.

Which notes exist, how they are indexed, and when they are deleted belongs to the
`issue-tracker` skill.

## Spike -- `docs/tasks/<label>/spike.md`

A spike document exists only on a spike branch. It takes the role of the specification for
the functionality the spike covers, and carries the same content the package or files level
comments would. In addition it may carry the open questions the spike exists to answer.

A spike ends in one of three ways:

* The branch is discarded and what was learned is written into the specification for a
  newly scheduled milestone, which is then implemented properly.
* The branch is discarded entirely, because the approach was wrong or the thing was
  infeasible.
* The spike became the production implementation. Its document becomes part of `docs/spec`
  as a real specification, and a milestone is added to the schedule already closed.

## See also

The `adr` skill for decisions on their way here, and the `issue-tracker` skill for
milestones, issues, and how a deferred note is indexed and retired.
