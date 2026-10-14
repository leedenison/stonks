# An event finds what it affects from the association

An association made through a MIC-derived identifier assumes that the value did not move
over the dates of the key's transactions. A fetch made under a MIC-derived identifier
assumes that the value did not move between the date of its data and the day of the
answer. A later event must find each assumption that it breaks. Later coverage must find
each assumption that it confirms.

Each assumption is derived from existing rows.

- An association records its identifier in `stated_keys.via_id`, and its transactions
  carry their dates. An event on a value finds the keys whose `via_id` names the value.
  It replays each key that has a transaction outside the recomputed validity.
- A fetch key records the identifier it sent and the moment of the answer. An event on a
  value finds the fetch keys that sent the value. It invalidates each data row whose date
  lies on the far side of the event from the fetch.
- `stated_keys.validity` is a cache. It is `confirmed` when every transaction of the key
  lies inside confirmed validity. New coverage, an event or an assertion
  recomputes it for the keys through the value. A recompute only writes the column.
- `resolution_keys` records the run that associated each key.

## Considered options

A record for each assumption, naming the run, the value, the interval and the key or
fetch key. It would let a run list what it assumed. It restates what validity and
`via_id` imply. Every write of coverage, an event or an assertion would have to keep
it in step. The statement details page and the coverage page show provisional
associations and gaps.
