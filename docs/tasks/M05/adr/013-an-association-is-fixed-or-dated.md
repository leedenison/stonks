# An association is fixed or dated

A stated key's association is fixed or dated.

A fixed association names one instrument and listing for all time, in `instrument_id`
and `listing_id`, as every association did before. A key has a fixed association when
its `via` is stable, when its `via` is a broker description, when its `via` is an option
symbol, or when the user chose for it. A transaction reaches its instrument through one
join on its key. An identifier event never touches a fixed association.

A dated association names a `MIC_TICKER` value, in `via_type`, `via_domain` and
`via_value`, instead of an instrument. Each of the key's transactions takes the
instrument that the value's validity names on the transaction's order date, and the
listing that carries the ticker on that day. Where the stated currency's family differs
from that listing's, the transaction takes the instrument's listing in the stated
family. A key has a dated association when the resolver associates it of its own accord
through a MIC-derived identifier. A `via` of `OPENFIGI_TICKER` or `DATASOURCE_TICKER`
dates the key through the `MIC_TICKER` on the same listing. Where the listing has none,
the association is fixed and provisional.

A CHECK allows exactly one of `instrument_id` and the `via_*` columns. An associated
key has an `arbiter`, as before.

An event changes the value's facts. A key's dates fall inside or outside its value's
validity as the facts stand at the read, so the key stays current when the value moves
or instruments merge.

## Considered options

A dated association that also records the current holder in `instrument_id`, patched on
each event. It is a cache that is wrong between the event and the patch, and every
write of facts must find the keys it affects.

## Consequences

- A key whose `via` is an option symbol (`OCC`, `OPTION`) is fixed and provisional until
  corporate events are built.
- A broker description `via` is fixed and provisional. Issue
  [010](../issues/010-broker-descriptions-in-the-admin-area.md) confirms it.
- The reassignment of a value is read, never written, so `stated_keys` has no validity
  column.
