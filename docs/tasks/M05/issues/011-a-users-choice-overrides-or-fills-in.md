---
title: A user's choice overrides or fills in
type: task
status: unreviewed
dependencies: [005]
---

## Scope

A user's choice pins the whole key to one instrument. See
[017](../adr/017-a-users-choice-pins-the-whole-key-for-now.md). That is known to be
insufficient.

- Distinguish a choice that overrides what a datasource said, because the user thinks it
  is wrong, from a choice that fills in a blank no datasource could provide.
- Decide how each interacts with validity, with later coverage and with an event on the
  value the key states.
- Replace the pin.
