---
title: Statements in the browser
type: task
dependencies: [006, 009]
---

## Scope

- An upload page that takes the broker's export, marshals it in the browser, shows the
  claimed period the marshaller derived and lets the user change it, and starts the
  upload.
- The run's progress and outcome on that page, with every rejected row and why.
- A history page listing the user's statements, each with its rejections.
- An e2e spec covering a statement with rejected rows and its appearance in the history.

The UI is clear that rows were rejected, both at upload time and when browsing history.
