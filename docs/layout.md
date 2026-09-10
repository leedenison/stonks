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
  auth/v1/auth.proto              stonks.auth.v1        -- sign in, session, sign out
  instrument/v1/instrument.proto  stonks.instrument.v1  -- instruments
  type/v1/type.proto              stonks.type.v1        -- shared types
```

Generated Go lands beside the `.proto` files (`paths=source_relative`) and is gitignored:
`auth/v1/auth.pb.go` for the messages and `auth/v1/authv1connect/` for the handlers and
clients. Generated TypeScript lands in `client/gen/` and `e2e/gen/`, also gitignored.

## `server/`

```
server/
  cmd/stonks/            the service binary; all wiring happens in main.go
  cmd/migrate/           applies the schema migrations to the database named by its argument
  cmd/healthcheck/       exits 0 when the URL named by its argument answers 200; the container healthcheck
  internal/              private packages
  pkg/                   packages with externally visible interfaces
```

## `client/`

```
client/
  app/                  routes; layout.tsx is the only server component
    components/         shared components, kebab-case filenames
    globals.css         the design system: theme tokens, dark mode, animations
  contexts/             React context providers, one file per concern
  hooks/                shared hooks
  lib/                  the transport, the typed clients, and pure utilities
  public/               static files served at /
  gen/                  protobuf-es output (generated, gitignored)
```

## `e2e/`

```
e2e/
  tests/                one spec per scenario
  helpers/              the shared test and its fixtures
  gen/                  protobuf-es output (generated, gitignored)
```

## `docker/`

```
docker/
  docker-compose.yml         base stack: postgres, redis, stonks, client, envoy
  docker-compose.dev.yml     dev overlay: bind mounts, live reload, observability
  docker-compose.test.yml    standalone stack for integration tests
  docker-compose.e2e.yml     e2e overlay: shifted ports, playwright service
  server/                    Dockerfile, Dockerfile.dev, air.toml
  client/                    Dockerfile, Dockerfile.dev, entrypoint.sh, dev.sh
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
  layout.md             this file
  schedule.md           scheduled, completed, deferred and spike milestones
  deferred/             unscheduled work items, and terminology.md, the terms
                        they are written in
  tasks/                task tracking files
```
