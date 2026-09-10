---
name: protobuf
description: Protobuf and Connect conventions for Stonks (proto/) -- package naming, enum rules, field documentation and protovalidate constraints. Use when adding or changing a proto file, an RPC or a field, or when deciding how a value crosses the wire.
---

# Protobuf

API definitions live in `proto/<area>/v1/<area>.proto`. Generated code is produced by
`make generate` and is gitignored on both sides.

## Naming

* Messages `PascalCase`, fields `lower_snake_case`, enums `PascalCase` with
  `UPPER_SNAKE_CASE` values.
* An enum's zero value is `<ENUM_NAME>_UNSPECIFIED`.
* Every RPC takes a dedicated request message and returns a dedicated response message,
  even when one is empty. This is what lets a field be added later without changing the
  signature. Do not use `google.protobuf.Empty`.

## Documentation

Document fields in the proto with a leading comment. The comment carries through to both
generated languages.

Say what the field means, and what an absent or empty value means. Do not describe the
type.

## Validation

Declare constraints with protovalidate rather than checking them in each handler:

```protobuf
string portfolio_id = 1 [(buf.validate.field).string.uuid = true];
```

The validating interceptor rejects a bad request before the handler, so a handler only has
to deal with what validation cannot express.

## See also

The `go` skill for the handlers, and the `typescript` skill for the generated client.
