---
title: Database tests of two packages create one datasource name
type: bug
---

`TestReplay` in `server/internal/replay` fails intermittently in `make db-test` while
creating the datasource `alpha`. The failure's text was not kept.

## Cause

`replay/dbtest_test.go` and `resolve/dbtest_test.go` each insert a datasource named
`alpha` inside the rolled-back transaction every database test runs in. `go test` runs
packages concurrently against one test database. While one package's transaction is
open, the other's insert waits on the `datasources` primary key. The name is the one
thing the two fixtures share with another package. The users the same fixtures create
carry a random suffix and never collide.

## Fix

Name each fixture's datasources per test, as the users are named.
