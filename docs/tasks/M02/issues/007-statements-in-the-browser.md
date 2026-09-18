---
title: Statements in the browser
type: task
dependencies: [009]
---

## Scope

- A transactions page, a placeholder until transactions are listed. Its empty state
  carries the upload button and the page is a drop target for an export.
- The upload dialog, opened from the transactions page. It takes the broker's export,
  marshals it in the browser, shows the broker detected, the row count, the claimed
  period the marshaller derived with the dates editable, and the first few rows, and
  starts the upload. It closes once the run is created.
- The activity sheet, opened from the icon in the top bar. It lists the user's runs
  newest first with the state of each, and an item expands to its rejections grouped by
  reason, each reason expanding to its rows and why. The badge on the icon signals a
  run that finished.
- A history page listing the user's statements, each with its rejections grouped the
  same way.
- An e2e spec covering a statement with rejected rows, its appearance in the activity
  sheet and its appearance in the history.

The UI is clear that rows were rejected, both when the run finishes and when browsing
history.

## Design

The dialog hands off to the sheet. CreateStatement answers with a pending run, so there
is nothing to watch in the dialog and the user is free to keep working.

Progress is the run's state, read by polling GetRun. A statement writes its transactions
and items in one database transaction, so there is nothing finer to show. A run reaching
a terminal state invalidates the transactions, holdings and statements queries.

The sheet is driven by ListStatements, so it survives a reload and shows runs started in
another tab. Runs of one user and broker proceed in order, so a queued upload appears as
pending behind the earlier one.

The sheet's items are runs and the UI calls them activity. "Event" names the economic
event a set of transactions are legs of.

The history page is the full list. A long list scans better as a page, each statement has
a URL, and the e2e spec has a stable surface to assert against. The sheet shows recent
activity and links through to it.

The dialog and the sheet carry test ids from the start. The e2e spec waits for a run to
reach a terminal state through the sheet.

The dialog stages, the sheet, the state chips and the placement of the transactions page
in the navigation are designed in [009](009-look-and-feel-of-the-user-and-admin-ui.md).

## Open questions

- How many runs the sheet lists, and whether older runs are reached only through the
  history page.
- The polling interval, and whether polling stops once every listed run is terminal.
