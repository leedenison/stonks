---
title: Offer candidates merged across datasources
type: task
status: unreviewed
---

## Motivation

A key that states a bare ticker is offered candidates for the user to confirm. Each
candidate is one datasource's group. A security that two datasources describe therefore
appears twice, and the user must choose between datasources' views of one security. A
confirmation takes the chosen group as the winner. The other groups attach to it
afterwards.

A resolution run compares the datasources' groups with each other before it writes. It
then attaches the groups that agree with the winner. The user should be offered the
result of that comparison: one candidate per security, carrying what every agreeing
datasource said about it.

## Scope

In:

- `Candidates` offers merged candidates. A merged candidate is a set of groups from
  different datasources. The groups agree with one another and share a stable
  identifier. The candidate carries the identifiers and listings of every group in the
  set.
- `Candidate` names the datasources that contributed, and a confirmation names the
  merged candidate it takes.
- A confirmation writes the merged candidate as a run writes a winner and the groups
  attached to it. Its highest precedence group wins and the others attach.
- The candidates dialog shows one row per merged candidate.

Out:

- Ranking the candidates by a guess, which is deferred.

## Open questions

- How groups merge when agreement is not transitive: two groups each agree with a third
  but disagree with each other. A run settles this by precedence once a winner exists.
- How a confirmation identifies a merged candidate, given that the datasources' answers
  may change between listing and confirming.
