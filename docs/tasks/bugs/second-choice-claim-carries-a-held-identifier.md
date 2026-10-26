---
title: A claim attached in the second choice can carry an identifier another instrument holds
type: bug
---

A resolution chooses twice when the write takes a stored instrument: once without the
database's claim, to find the instrument, and once with it. The write target is found from
the identifiers of the claims the first choice attached. A claim the first choice dropped
can attach in the second choice through a stable identifier it shares with the stored
instrument. Its other identifiers were never looked up. Where one of them identifies
another stored instrument, the write fails on the unique constraint over identifiers
instead of merging the instruments or dropping the claim.

To reproduce in the resolve dbtest stack: stored X carries a FIGI and an ISIN and no
ticker, stored Y carries a CUSIP. Resolve a key stating a venue ticker where alpha answers
the FIGI and the ticker on one day and beta answers the ISIN, the CUSIP and the ticker on
the next day. Beta is dropped at corroboration in the first choice, attaches to X through
the ISIN in the second, and writes the CUSIP that Y holds.
