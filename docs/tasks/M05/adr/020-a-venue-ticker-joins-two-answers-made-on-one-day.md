# A venue ticker joins two answers made on one day

A resolution asks every datasource about a stated key and chooses among their claims by
precedence. A claim below the winner attaches only when it corroborates the winner, which
means that it shares a stable identifier with the winner. Datasources answer a ticker
lookup with disjoint identifier types. OpenFIGI and Massive return FIGIs. EODHD returns an
ISIN and a CUSIP, and the system leaves its FIGI unread. For a key that states only a venue
ticker, EODHD's claim therefore never shares a stable identifier with OpenFIGI's, and it is
dropped at the corroboration step on every first sight. The instrument is written without
its ISIN and CUSIP. A later key that states the ISIN fetches again for identifiers the
system already received and discarded.

The system already trusts a datasource's answer on a day. Attaching a claim writes an
assertion of each identifier it carries, on the UTC day of the answer. Validity takes one
datasource's assertion as what the ticker names that day, and precedence decides where two
datasources disagree. A MIC_TICKER names one instrument on one day. Two answers made on
one day that both carry one MIC_TICKER therefore describe one instrument, and the ticker
joins them as a stable identifier would.

A claim corroborates the winner where it shares a stable identifier with the winner, or
where both are datasource answers made on the same UTC day and both carry the same
MIC_TICKER. The join needs two answers. The database's ticker row names the present
holder and is not an answer made on a day, so a claim does not join the database's
instrument through a ticker. The contradiction check runs first and is unchanged: the
precedence step drops a claim that contradicts a chosen claim, whatever the claim shares
with the winner. The corroboration finding states both tests: the claim and the winner
share neither a stable identifier nor a venue ticker answered on one day.

A claim joined through the ticker alone does not choose the instrument the write takes.
The key's identifiers and the identifiers of the claims that share a stable identifier
with the winner find the stored instrument. Where a stable identifier of a claim joined
through the ticker alone identifies another stored instrument, the claim is dropped with a
contradiction finding, whether the write creates the instrument or takes a stored one. Two
stored instruments therefore never merge through a ticker, so an identifier event never
unwinds a merge.

The rule uses evidence the system already accepts rather than discarding it. The risk is a
datasource whose data is stale across a reassignment. Where the system does not hold the
old instrument, its answer writes the old instrument's stable identifiers onto the new
one, and an identifier row is undated, so the mistake is not bounded by a day as an
assertion is. Validity takes the same risk on the same answers. Each identifier row
records the fetch key that wrote it, so a stale answer can be traced and its rows removed.

## Considered options

- Fetch again under a stable identifier of the winner that the lower datasource serves.
  EODHD serves a composite FIGI, which OpenFIGI and Massive both return. The second answer
  would name the identifier sent and corroborate through it. It costs a call against
  EODHD's quota of twenty a day and serialises the fetch by precedence.
- Let a claim joined through the ticker alone choose the write target, as a claim that
  shares a stable identifier does. Where its stable identifier identifies another stored
  instrument, the write would merge that instrument into the target through the ticker. A
  merge relinks keys and deletes listings, and an identifier event never unwinds one.
- Attach the claim and leave out the identifiers another instrument holds, as a refused
  merge leaves them out. The write would still assert the left-out identifier on the
  target, and a partial attachment states that the answer describes the target while one
  of its identifiers says otherwise.

## Consequences

- A first sight through a venue ticker writes the ISIN and CUSIP that EODHD returns. A key
  that later states the ISIN matches the instrument in the database without a fetch.
- A claim is still dropped at corroboration when it carries the ticker at another venue,
  when its answer fell on another UTC day, or when the winner is the database's instrument.
  A claim joined through the ticker alone is also dropped at the write where a stable
  identifier it carries identifies another stored instrument. The held instrument can
  therefore still lack an answer's identifiers. Extending validity relies on that.
- Only a MIC_TICKER joins. The other MIC-derived identifier types lack validity, and one
  datasource returns each of them, so two answers never share one.
