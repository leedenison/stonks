# Extending validity writes only the held ticker's assertions

An extend validity run carries each held ticker's validity to the present, as
[005](005-validity-is-bootstrapped-from-the-present-backwards.md) states. A held ticker is
the MIC_TICKER through which a stated key is dated, where a transaction names the key. An
unresolved key lacks facts to carry forward, and
[008](008-a-new-fact-replays-the-unresolved-keys-that-state-its-ticker.md) covers it. A
stable identifier fixes a key for all time, so that key does not need validity.

An identity answer to an extend validity run writes only assertions of the held ticker,
on the day of the answer:

- An answer stating that the datasource knows nothing under the ticker writes a negative
  assertion.
- A listing that states the ticker at a venue of its operating MIC writes a positive
  assertion. It names the instrument of the ticker's latest interval where the listing
  carries another identifier that the instrument stores. Otherwise it names the stored
  instrument of the listing's first stored stable identifier.
- An answer that lists the ticker nowhere, or matches no stored instrument, writes
  nothing. Extending validity never creates an instrument.

A resolution writes identifiers, listings, ticker moves, identity coverage and the fetch
key's instrument for its user's key. An extend validity run acts for the system rather
than for a key, so its answers write only assertions.

The latest interval's instrument comes first because a datasource may answer with an
identifier that the instrument does not carry as a stable identifier. EODHD answers for a
non-US venue with a DATASOURCE_TICKER and, for some rows, without an ISIN. A resolution may
also have dropped the answer because it matched none of the winner's stable identifiers.
The held instrument then lacks the answer's identifiers. A new instrument made from them
would contradict every later extend validity run.

## Considered options

- Write what `attach` writes for a resolution. It needs a user for the fetch key and the
  coverage, and it would move identifiers between instruments without a key to justify the
  move.
