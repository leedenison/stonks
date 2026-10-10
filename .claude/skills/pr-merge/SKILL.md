---
name: pr-merge
description: How a pull request is squashed, opened, rerun and merged, alone or in a stack, and the rate at which CI runs may start so Docker Hub does not refuse the image pulls. Use when opening a pull request, merging one, rerunning its checks, or working through a stack of them.
---

# Pull Requests

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

Open a stacked pull request on its parent branch, with `gh pr create --base <parent>`.
The description names the issue and the position in the stack ("Second of four PRs for
M04 issue 004. Stacked on #150."), then says what the change does in prose, and ends with
the attribution line.

## Merging

Merge with `gh pr merge <n> --merge`. Never pass `--delete-branch`: the repository
deletes the branch as part of the merge, and that merge-linked deletion is what retargets
a child pull request to main. Merge a stack parent first, wait for the child to retarget,
then merge the child. The `gates` check is the only required one, and a branch need not
be current with main to merge.

## Reruns

Rerun with `gh run rerun <id> --failed`, so only the failed jobs run again. Rerun only a
failure the change did not cause: a flake that has an issue under `docs/tasks/bugs/`, or
an image pull that Docker Hub refused. When Docker Hub answers a pull with
`429 Too Many Requests`, the pull limit is the cause. A rerun in the same six-hour window
fails the same way.

## Rate

Every job pulls its own base images, so one CI run costs about twenty-five Docker Hub
pulls. A Docker Personal account is allowed two hundred pulls in six hours. Start at most
one run in any forty-five minutes, counting a push, a new pull request and a rerun
alike. Keep at most one run in flight. Open a stack one pull request at a time, each
after the previous one's run has finished.
