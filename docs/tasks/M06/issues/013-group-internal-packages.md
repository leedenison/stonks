---
title: Group the internal packages
type: task
---

## Scope

The packages under `server/internal` move into groups of related packages. A move keeps
the name of each package.

| Group | Packages |
|---|---|
| `platform/` | `config`, `logger`, `telemetry`, `rdb`, `ptr` |
| `db/` | `migrations` |
| `identity/` | `resolve`, `validity`, `attest`, `coverage`, `group` |
| `run/` | `replay`, `extendvalidity`, `refreshcorporateevents` |

`platform/` and `identity/` only group other packages. `db` and `run` stay packages.
The remaining packages stay at the top of `internal`.

In:

- Every import path, in the Go code and its tests.
- The paths in `sqlc.yaml`, `.golangci.yml` and the `//go:generate` directives.
- The links in the docs and the issues to the files that move.
- `docs/layout.md` lists the groups.
- `go vet -tags=dbtest` passes, since plain `go vet` skips the dbtest files.
