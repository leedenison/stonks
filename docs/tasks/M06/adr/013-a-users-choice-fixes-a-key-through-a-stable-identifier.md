# A user's choice fixes a key through a stable identifier

`ConfirmCandidate` lets a user choose a candidate for a key that lacks an association.
For a key that states only a venue ticker, the writer skips dating when the arbiter is
`user`. The key becomes fixed with `via_id` on a ticker row. Under
[007](007-identifiers-hold-identity-and-a-ticker-holds-only-its-spelling.md) that row does
not exist.

The confirmation names one of the candidate's identifiers. A candidate carries a stable
identifier in almost every case. The write creates its `identifiers` row. The key is
fixed through that identifier, with the arbiter `user`. A key that states only a venue
ticker is fixed the same way. A candidate that carries only a ticker cannot be confirmed.

A key has three states:

| State | instr_id | via_identifier_id | via_ticker_id, via_market, via_venue | arbiter |
|---|---|---|---|---|
| Unresolved | NULL | NULL | NULL | NULL |
| Fixed | set | an identity row | NULL | any |
| Dated | NULL | NULL | the stated venue ticker | not `user` |

`via_id` becomes `via_identifier_id`, so each `via_` column names the table it
references. CHECK constraints hold `instr_id` and `via_identifier_id` both set or both
NULL, the three ticker columns all set or all NULL, and a NULL instrument wherever a
ticker is set.

D-ANNOT records a choice for a run of a dated key's transactions, as a row on the key
that derivation reads before ticker validity. It is not a key state. It stays deferred.

## Considered options

- A fourth state, fixed through a ticker with the arbiter `user`. It gives the schema a
  state that D-ANNOT's run-scoped choice later removes.
