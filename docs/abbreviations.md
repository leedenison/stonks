# Abbreviations

Code names use one standard abbreviation for each long word in common use.

| Word | Abbreviation |
|---|---|
| corporate | `corp` |
| corporate action | `corp act` |
| instrument | `instr` |
| statement | `stmt` |
| transaction, in the financial sense | `txn` |

## Where they apply

The abbreviations apply to every name in the code:

- Identifiers in Go, TypeScript, protobuf and SQL, with their compounds and plurals:
  `InstrID`, `instr_id`, `ListInstrs`, `stmt_keys`, `TxnMark`.
- File, directory, package and proto package names: `internal/stmt`, `stonks.stmt.v1`,
  `003_instrs.sql`.
- URL paths: `/stmts`, `/txns`, `/admin/instrs`.
- String values that act as names: test ids, `data-*` values, enum values, query keys,
  metric names and log categories.

Prose, comments and UI labels keep the full words. `enumLabel` in `client/lib/enum.ts`
spells out each abbreviation when it renders an enum value as a label, so its table holds
every abbreviation above.

A local variable that would shadow a package with an abbreviated name takes the name of its
role, such as `row` or `run`.

## Exceptions

These keep the full word:

- A database transaction. Its name is `tx`.
- An OpenTelemetry instrument, which is a counter or another metric.
- A name that a third party defines, such as `redisotel.InstrumentTracing` or the goose
  `StatementBegin` annotation.
- A name that an external format defines, such as the Fidelity column `Transaction type`
  or the Schwab key `BrokerageTransactions`.
- The name of a document under `docs/`.
