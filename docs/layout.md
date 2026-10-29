# Repository Layout

| Path | Contents |
|---|---|
| `proto/` | Protobuf API definitions, and the Go code generated from them |
| `server/` | The Go backend service |
| `client/` | The Next.js front end |
| `e2e/` | Playwright end-to-end suite |
| `docker/` | Dockerfiles, compose stacks, and the configuration of every supporting service |
| `docs/` | Terminology, specification, milestones and deferred notes |
| `scripts/` | Small shell helpers invoked by the Makefile |
| `local/` | Developer-local files. Gitignored. |

## `proto/`

```
proto/
  admin/block/v1/block.proto                                        stonks.admin.block.v1                    -- datasource blocks, across every user
  admin/coverage/v1/coverage.proto                                  stonks.admin.coverage.v1                 -- where each ticker's validity is confirmed, and identifier attestations of what no datasource covers
  admin/datasource/v1/datasource.proto                              stonks.admin.datasource.v1               -- the external providers and their settings
  admin/instr/v1/instr.proto                                        stonks.admin.instr.v1                    -- every instrument, with its identifiers and listings
  admin/replay_run/v1/replay_run.proto                              stonks.admin.replay_run.v1               -- replay runs, started by an administrator
  admin/run/v1/run.proto                                            stonks.admin.run.v1                      -- what runs did, across every user, and the system runs an administrator starts
  auth/v1/auth.proto                                                stonks.auth.v1                           -- sign in, session, sign out
  holding/v1/holding.proto                                          stonks.holding.v1                        -- holdings
  run/v1/run.proto                                                  stonks.run.v1                            -- runs
  stmt/v1/stmt.proto                                                stonks.stmt.v1                           -- the neutral format of a statement, its ingestion and its outcome
  type/v1/type.proto                                                stonks.type.v1                           -- shared types
```

A service open to administrators alone nests under `admin/`, so one rule in the handler
chain gates every procedure of a `stonks.admin` package.

Generated Go lands beside the `.proto` files (`paths=source_relative`) and is gitignored:
`auth/v1/auth.pb.go` for the messages and `auth/v1/authv1connect/` for the handlers and
clients. Generated TypeScript lands in `client/gen/` and `e2e/gen/`, also gitignored.

## `server/`

```
server/
  cmd/stonks/            the service binary; all wiring happens in main.go
  cmd/migrate/           applies the schema migrations to the database named by its argument
  cmd/healthcheck/       exits 0 when the URL named by its argument answers 200; the container healthcheck
  cmd/vcrproxy/          the e2e stack's provider: a reverse proxy replaying recorded traffic
  internal/              private packages
    platform/            process plumbing: configuration, logging, telemetry, Redis, pointer helpers
    db/                  the database: queries, generated code, migrations
    identity/            how a stated key comes to name an instrument: resolution, ticker validity,
                         identifier attestation, coverage, the holdings of keys without an instrument
    run/                 runs and the work they do: replay, ticker reassertion, corporate action refresh
  pkg/                   packages with externally visible interfaces
```

A package that belongs to none of these groups sits at the top of `internal/`.

## `client/`

```
client/
  app/                  routes; layout.tsx is the only server component
    (app)/              the user pages, guarded by their layout; no URL segment
    admin/              the admin area, guarded by its layout and the admin role
    components/         shared components, kebab-case filenames
    globals.css         the design system: theme tokens, dark mode, animations
  contexts/             React context providers, one file per concern
  hooks/                shared hooks
  lib/                  the transport, the typed clients, and pure utilities
    marshal/            the marshallers reading broker exports and the standard format into the neutral format
    upload/             parsing a file chosen in the browser
  public/               static files served at /
  gen/                  protobuf-es output (generated, gitignored)
```

## `e2e/`

```
e2e/
  tests/                one spec per scenario
  helpers/              the shared test and its fixtures
  fixtures/             the files a spec uploads
  gen/                  protobuf-es output (generated, gitignored)
```

## `docker/`

```
docker/
  docker-compose.yml         base stack: postgres, redis, stonks, client, envoy
  docker-compose.dev.yml     dev overlay: bind mounts, live reload, observability
  docker-compose.test.yml    standalone stack for integration tests
  docker-compose.e2e.yml     e2e overlay: shifted ports, the vcrproxy provider, playwright service
  server/                    Dockerfile, Dockerfile.dev, air.toml
  client/                    Dockerfile, Dockerfile.dev, entrypoint.sh, dev.sh
  vcrproxy/                  Dockerfile and the cassettes it replays
  envoy/                     envoy.yaml, envoy.dev.yaml
  otel-collector/            config.yaml
  prometheus/                prometheus.yml
  grafana/                   provisioning/ and the dashboards it serves
```

One base compose file, and one overlay per purpose. A service with a development or e2e
variant carries a Dockerfile per variant in a directory named for the service, and each
supporting service's configuration sits in a directory named for that service.

## `docs/`

```
docs/
  abbreviations.md      the standard abbreviation of each long word in code names
  layout.md             this file
  schedule.md           scheduled, completed, deferred and spike milestones
  deferred/             unscheduled work items, and terminology.md, the terms
                        they are written in
  tasks/                task tracking files
```
