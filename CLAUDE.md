## **Project Overview**

See `README.md`.

## **Project Status**

This project is pre-release. Datamodels, APIs, protobuf definitions, etc are not
considered stable. Changes to these artifacts should not create new migrations, reserve
protobuf fields or account for backwards compatibility.

Retired proto fields do not reserve their number. Delete the field outright, renumber the
message so its field numbers run from 1 without gaps.  Do not add comments referring to 
the change or the removed field.

## Tech Stack

### CLAUDE.md and skills

Do not edit CLAUDE.md or .claude/skills without explicit agreement from the user.

### Front End

* Next.js (TypeScript), App Router
* Tailwind CSS
* TanStack Query for server state
* Recharts (charting)
* Connect (`@connectrpc/connect-web`) over the generated protobuf-es types

### Back End

* Go, serving protobuf over Connect (`connectrpc.com/connect`)
* Envoy at the edge
* PostgreSQL with the TimescaleDB timeseries extension
* Redis for sessions
* OpenTelemetry, collected by an OTel Collector and read in Grafana

## Development Setup

1. Copy `.env.example` to `.env` and fill in `GOOGLE_OAUTH_CLIENT_ID`.
2. `make run` starts the development stack with live reload for both the server and the
   client. The application is served at http://localhost:8080.
3. `make check` runs every non-test gate.
4. `make test` runs the unit, client and integration tests.
5. `make e2e-test` runs the full-stack Playwright suite.

Every build, test and lint step runs in a container.

## Key Documentation

* docs/layout.md - Repository directory layout
* docs/schedule.md - Scheduled, completed, deferred and spike milestones
* docs/tasks/ - Open issues and decisions, one directory per milestone
* docs/deferred/ - Work items that are not yet scheduled

## Pull Request Guidelines

Prefer smaller, focused PRs to reduce review burden:

* Target size: 500-800 lines changed
* Maximum: Going over is acceptable when necessary, but avoid PRs exceeding 1000 lines if
  they can be split

### Before opening a PR

Run `make check`, `make test` and `make e2e-test` and get them passing.

A test that fails for a reason the change did not cause is still a result to act on: say
so in the PR description, with the evidence that it fails on an unmodified tree.

### Merging

A pull request arrives as one commit. Squash the branch before pushing it, and before
branching the next pull request from it:

```
git reset --soft origin/main && git commit
```

Squashing a branch another branch already descends from orphans the descendant, so the
squash comes first. A review that lands once the child branch exists takes a second
commit.

Merge with `gh pr merge <n> --merge`.

**Never pass `--delete-branch`.** The repository has `delete_branch_on_merge` enabled, so
the branch is removed as part of the merge, and that merge-linked deletion is what
retargets any PR based on the branch.

Merge a stack parent first, one at a time, and let each merge retarget the next.

### Branching Workflow

When a plan calls for multiple PRs, create and complete each PR on its own feature branch
before starting the next. Do not implement all changes on a single branch and attempt to
separate them afterward -- this is error-prone and creates unnecessary rework.

### Worktrees

Whenever you begin work in a new worktree you should:
1. Copy the `.env` file from the root of the repo
2. Copy the `local` directory from the root of the repo, if there is one
3. Run `make generate` to generate the protobuf bindings, SQL bindings and mocks

## Naming

Prefer terse names when naming functions and variables.

## Plain English

Use plain English when writing comments and documentation.  In addition to your
own judgement apply the following rules:

- Do not write sentences in which the later clause states the inverse of the earlier
  clause.
- Do not strand prepositions. A relative clause or an embedded question must not end in
  a preposition whose object is the noun it modifies or the question word. A preposition
  that completes a verb ("left out", "signs everyone out", "reading on") is part of the
  verb and is fine.
- Do not pack a condition into the subject as a reduced relative clause with a passive
  predicate. Write the condition as a subordinate clause with a named actor and an
  active verb. A sentence that defines a term ("A hit is an instrument the re-read
  found") is fine.
- Prefer short sentences with one clause per sentence.
- Do not use 'no negation' (eg. "writes no block") unless it is idiomatic (eg. "takes
  no notice").

## Documentation

Keep documentation short and to the point.  Avoid repetition.  Write in an expository
style not a narrative style.  

Important: Do not explain any idea in reference to any previous state or prior art in
this repository; explain it standalone as it is today.

Never use smart quotes in documentation, comments or plans.

Important: When a change includes documentation you must execute a Sonnet agent
to ensure it complies with the Plain English, Documentation and Personal Data rules.
The agent reports violations and does not edit. Then fix any errors highlighted before
telling the user that the change is ready for review.

## Code Comments

Important: When a change includes comments you must execute a Sonnet agent
to ensure it complies with the Plain English, Documentation and Personal Data rules and
with the `comments` skill. Where the change touches a package or file comment, the agent
applies the skill's package level test to each paragraph of it. The agent reports
violations and does not edit. Then fix any errors highlighted before telling the user
that the change is ready for review.

## Personal Data

The repository must contain no personal data belonging to a real person. This applies to
code, tests, fixtures, comments, commit messages, issues, specs and plans alike. `local/`
is gitignored and is the only place where real account exports belong.

Never commit any of the following:

* Real people's names, including in a comment describing where test data came from, in an
  issue discussing it, and in the name of a file that holds it. Refer to the account or the
  export rather than to whoever owns it.
* Real broker account numbers, and any identifier that embeds one.
* Real transaction, order or statement reference numbers issued by a broker.
* Real email addresses, postal addresses and telephone numbers.
* Unredacted recordings of external HTTP traffic. A recording captures the credentials in
  the request and whatever the provider returned in the response, so both are redacted as
  it is saved rather than cleaned up afterwards.

Public security identifiers -- tickers, ISINs, CUSIPs, MICs, exchange codes -- are
reference data rather than personal data, and are fine to use as they are.

Test data modelled on a real export is the normal way to pin down a format, and it stays
welcome: copy the shape, the column order and the quirks, then replace every account number
and reference with an invented one before committing. Invent them so that the properties
the test depends on survive -- distinctness, ordering, and the numeric distance between
references where a test compares them -- and say in a comment that the file is modelled on
a real export with its identifiers replaced, so the next reader does not restore them from
the original. Amounts, dates and instruments carry no identifier and may stay as they are.
