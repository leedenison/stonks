---
title: Name ticker validity and identifier attestation in full
type: task
---

## Scope

Validity is a property of a ticker, and an attestation is an administrator's assertion
of an identifier. Names that say "validity" or "attestation" alone gain the noun.

| Now | Becomes |
|---|---|
| package `identity/validity` | `identity/tickervalidity` |
| `broken_validity_bracket`, `FindingKindBrokenValidityBracket` | `broken_ticker_validity_bracket`, `FindingKindBrokenTickerValidityBracket` |
| `HasValidity` | `HasTickerValidity` |
| an unqualified `Attestation` in a type or function name | `IdentifierAttestation` |

In:

- Go, proto, SQL and the client.
- Prose, comments and UI labels say "ticker validity" and "identifier attestation".

Local variables keep their short names. `attested_by` and `Attested` describe an
assertion, and keep their names. The DOM's `ValidityState`, `checkValidity` and
`setCustomValidity` are outside the scope.
