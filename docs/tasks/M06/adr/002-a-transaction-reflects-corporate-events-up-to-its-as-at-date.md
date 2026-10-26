# A transaction reflects corporate events up to its as-at date

A transaction as at day d is stated in the units that follow every corporate event with
an ex-date on or before d. A trade on an ex-date is in post-split units, as exchanges
quote it. Restating the transaction to today multiplies its quantity by the ratio of each
corporate event with an ex-date after d and on or before today. A corporate event with a
later ex-date is stored and takes effect when its ex-date passes.

The system stores only raw quantities. Holdings compute the restated quantity on read.
Ratios multiply as exact rationals. Each transaction is restated exactly, the sum is
exact, and the result is rounded once.

## Consequences

A stored adjusted quantity goes stale whenever a corporate event arrives late, a fetch
fails part way, a corporate event is deleted or a future ex-date passes. Computing on
read avoids each of these. It also needs no scheduled job to apply a split on its
ex-date.

A later corporate event changes only a derived value. The system therefore never refuses
data because a future corporate event could leave it wrong.
