---
name: pr-merge
description: How a pull request is squashed, opened and merged, alone or in a stack, and when CI is run by hand. Use when opening a pull request, merging one, running CI, or working through a stack of them.
---

# Pull Requests

## Review

Build a plan with several pull requests as a stack of branches. Each branch starts from
the one before it. Each change is committed on its own branch and not pushed. The user
reviews the stack locally. Review fixes follow the rules in One commit.

After review, open the pull requests from the bottom of the stack. Push the lowest branch
and open its pull request on main. Merge it; CI says when a run must be green first. Then
fetch, run `git rebase origin/main` on the next branch, and open its pull request. The
rebase replays only that branch's own commits, because its parent's tip is an ancestor of
main.

## One commit

A pull request arrives as one commit. Squash the branch before pushing it, and before
branching the next pull request from it:

```
git reset --soft <parent> && git commit
```

The parent is the commit the branch descends from: `origin/main` for a lone branch and
the parent branch's tip for a stacked one. The reset keeps the branch's tree. Onto a
parent that has gained other commits, it makes a commit that reverts them. To move a
branch onto such a parent, use `git rebase --onto <new parent> <old parent> <branch>`,
which carries only the branch's own commits.

Squash a branch before any child branches from it, because squashing a parent orphans
the commits of its children. A review fix on a branch that already has children is a
second commit, never an amend. Rebase each child onto the new parent tip, parent first.

## Before opening

`make check`, `make test` and `make e2e-test` pass on the branch. A test that fails for a
reason the change did not cause is still a result to act on: say so in the description,
with the evidence that it fails on an unmodified tree. If a spec needs a call its
recording lacks, run `make e2e-record` first. The target appends only the missing calls.

## Opening

Open a pull request on main. If the parent branch is still open, open it on the parent
with `gh pr create --base <parent>`. The description names the issue and the position in
the stack ("Second of four PRs for M04 issue 004. Stacked on #150."), then says what the
change does in prose, and ends with the attribution line.

## Merging

Merge with `gh pr merge <n> --merge`. Never pass `--delete-branch`: the repository
deletes the branch as part of the merge, and that merge-linked deletion is what retargets
a child pull request to main. Where a child pull request is open on its parent, merge
the parent first and wait for the child to retarget before merging it.

## CI

CI runs only when someone starts it by hand, and merging depends only on review. Before
merging the last pull request of a stack, or one that stands alone, start a run on its
branch:

```
gh workflow run CI --ref <branch>
```

Merge the pull request once that run is green. The other pull requests of a stack merge
without a run, since the last one's run covers the tree they build up. `make check`, `make
test` and `make e2e-test` pass locally on every branch before its pull request opens.