# A key resolves as matched, rejected, unrecognised or unavailable

A resolution records one of four outcomes for each stated key.

- Matched: the key is associated with an instrument, and the resolution wrote the
  association in the transaction that wrote what the response named.
- Rejected: the key contradicts the reference data it names, and its rows are refused.
  Only a currency key can be rejected: one naming a code the seed lacks, one stating a
  class disjoint from cash, or one stating a currency its instrument has no listing in.
  A security key contradicting a database instrument is unrecognised instead, so its
  rows are kept and grouped, and the same statement is not refused depending on whether
  another user resolved the security first.
- Unrecognised: the key was attempted and nothing named it, or every candidate was
  dropped, or nothing it states can associate. A key stating only a bare ticker is sent,
  since a datasource may serve it, but never associates through it.
- Unavailable: nothing served the key and a datasource serving it failed or was
  blocked, temporarily or permanently. A cleared block is what replay re-tries, so a
  permanent failure counts.

Unrecognised and unavailable leave the key unresolved, grouped with the user's other
unresolved keys, and may carry a reason saying why. Rejected always carries one. Matched
never does.
