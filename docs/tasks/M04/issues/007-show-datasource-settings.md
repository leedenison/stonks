---
title: Show a datasource's endpoint and credential
type: task
status: unreviewed
dependencies: [002]
---

## Scope

The datasources admin page shows every setting a datasource holds as it is. An
administrator reads and edits the endpoint and the credential as plain text fields.

In:

- The admin API returns a datasource's credential.
- The page shows the endpoint the integration calls, including the provider's default
  when the row names none.
- The edit dialog prefills the endpoint and the credential. Saving sends what the
  fields hold, and an empty field clears the setting.

Out:

- Who may read a credential. The admin pages are open to administrators alone.

## Motivation

An administrator needs to check which key and address each datasource uses. Showing both
as plain fields makes that check direct and lets the administrator edit either in place.
