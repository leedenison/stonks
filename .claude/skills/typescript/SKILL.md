---
name: typescript
description: TypeScript and Next.js conventions for the Stonks client (client/) -- using generated protobuf-es types, TS strictness, filenames, styling tokens, and lint/format. Use when writing or changing client code, calling the API from the browser, or adding a component, hook or query.
---

# TypeScript

The front end lives in `client/`, a Next.js App Router application. Run everything through
the make targets: `make client-typecheck`, `make lint-ts`, `make fmt-check`,
`make client-test`.

## Style

Filenames are kebab-case.

Prettier formats every TypeScript file. `make fmt` rewrites, `make fmt-check` gates, and
`make check` runs the gate. `client/` and `e2e/` are separate npm projects.

`strict` stays on. No `any`; use `unknown` and narrow it. No non-null assertions to
silence the checker.

ESLint carries no formatting rules; it is there for correctness alone. `npm run lint` is
`eslint --max-warnings 0 .` -- warnings are failures.

## Styling

Never write a raw colour in a component. If no token provides what is needed, add one.

## Generated types

Protobuf types come from `client/gen`, produced by `make generate` and gitignored. Import
them through the `@/gen/...` alias.

Never hand-write a procedure name string. `createClient(PortfolioService, transport)`
takes the generated service descriptor, so a renamed RPC is a compile error rather than a
runtime 404.

Construct messages with `create(SchemaName, {...})` rather than object literals.

## See also

The `protobuf` skill for the contract these types come from, and the `unit-testing` and
`e2e-testing` skills.
