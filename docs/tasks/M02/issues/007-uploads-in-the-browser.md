---
title: Uploads in the browser
type: task
dependencies: [006, 009]
---

## Scope

- An upload page that takes the broker's export, marshals it in the browser and starts
  the upload.
- The run's progress and outcome on that page, with every rejected row and why.
- An upload history page listing the user's uploads, each with its rejections.
- An e2e spec covering an upload with rejected rows and its appearance in the history.

The UI is clear that rows were rejected, both at upload time and when browsing history.
