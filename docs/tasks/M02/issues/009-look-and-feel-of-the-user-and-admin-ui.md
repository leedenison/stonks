---
title: Look and feel of the user and admin UI
type: task
---

## Scope

The shell and page patterns the user and admin pages are built on, settled before the
first of them is built.

In:

- The application shell: navigation between the user pages, where the admin area sits
  and how an administrator reaches it, and what a non-admin sees of it.
- Page templates for the shapes the initial functionality needs: a table of rows, a form
  that starts work, and a view of a run in progress and its outcome.
- How a rejected row, an empty table, a loading state and an error are presented, so
  the upload, history and holdings pages present them the same way.
- The admin pages that follow this milestone, runs, findings and datasources, sketched
  to the same patterns so the shell does not change when they arrive.
- The design as a canvas or mockups covering each page of issues
  [007](007-statements-in-the-browser.md) and [008](008-holdings-in-the-browser.md), and
  any change to the `frontend-design` skill the decisions require.
- Applying the `frontend-design` skill to the existing UI: the shell, the sign-in and
  profile pages and the error and not-found pages, so the tokens, dark mode, typography,
  density and motion it fixes are what the new pages inherit.

Out:

- Building the upload, history and holdings pages.
