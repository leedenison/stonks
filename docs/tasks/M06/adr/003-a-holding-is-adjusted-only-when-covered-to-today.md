# A holding is adjusted only when covered to today

A transaction is covered when one datasource and market span the period from its as-at
date to today. See
[001](001-a-corporate-action-is-stored-against-its-ticker-and-attributed-through-validity.md).
A holding shows its restated quantity when every one of its transactions is covered.
Otherwise it shows its raw quantity, marked as unadjusted. The holding detail shows the
period that lacks coverage.

Coverage ends on the day the datasource answered. A cached answer ends it on the day of
the original call. A holding is therefore unadjusted from the day after the last answer
until the next one. This is intended until scheduled fetches exist.

Only an instrument of a class that corporate actions restate takes part. Stocks, funds
and similar classes carry a trait for this in `internal/types`. Cash, currencies and
options show their raw quantity without the mark.

## Considered options

- Restate each covered transaction, keep the rest raw, and mark the sum as partial. The
  number is often closer to the truth, but it mixes units within one sum.
- Treat coverage as reaching today for some days after a fetch. This is a scheduled
  fetch in all but name.
