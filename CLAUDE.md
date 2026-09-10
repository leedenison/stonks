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

Always squash: `gh pr merge <n> --squash`. **Never pass `--delete-branch`.** The
repository has `delete_branch_on_merge` enabled, so the branch is removed as part of the
merge, and that merge-linked deletion is what retargets any PR based on the branch. An
explicit ref deletion is a plain branch deletion instead, which **closes** dependent PRs
rather than retargeting them.

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

## Documentation

Keep documentation short and to the point.  Avoid repetition.  Do not write sentences in
which the later clause states the inverse of the earlier clause.  Write in an expository
style not a narrative style.  

Important: Do not explain any idea in reference to any previous state or prior art in
this repository; explain it standalone as it is today.

Never use smart quotes when generating documentation or plans.

Important: When you have completed a change that includes documentation you must review it
against the documentation and personal data rules in Claude.md and relevant skills before
committing to the repository.

## Code Comments

Keep comments short and to the point.  Avoid repetition.  Do not write sentences in
which the later clause states the inverse of the earlier clause.  Write in an expository
style not a narrative style.  

Important: Do not explain code or functionality in reference to any previous state or prior
art of this repository's code; explain it standalone as it is today.

Do not refer to project tasks or milestones in comments.

Comments should only explain what is not already obvious from the code. 

- Comments must focus on the most important elements of code being described.  Do **NOT** add
  comments to code describing the change you just made simply because you made the change.
  Always evaluate whether the comment meets the important threshold.
- Comments on packages explain the large scale design choices captured in the package in
  terms of invariants maintained, constraints adhered to and conventions followed.
- Comments on type definitions should explain what real world concepts are being modelled
  and what each field represents when these are not obvious from the names chosen, or there
  is some subtlety that the reader may not expect.  Illustrate example values when valid
  values are more restrictive than the type itself implies.
- Comments on functions or RPCs should explain the behaviour of the function when it is not
  obvious from the name chosen.  Parameters should only be explained if their use or handling
  of their value is surprising in some way.  Return values should only be explained if their
  values are surprising under some circumstances.  Error values should only be explained if
  the cases when they are raised are surprising or the mechanism is surprising (eg. they
  cause a panic).
- Inline comments are reserved for code that must be a certain way because of external
  factors.

Important: When you have completed a change that includes code comments you must review it
against the code comments and personal data rules in Claude.md and relevant skills before
committing to the repository.

## Personal Data

The repository must contain no personal data belonging to a real person. This applies to
code, tests, fixtures, comments, commit messages, issues, specs and plans alike. `local/`
is gitignored and is where real account exports belong; it is the only place they belong.

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
