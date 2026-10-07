# Only a complete provider contradicts a stated currency

A provider's coverage is complete when the provider lists every listing an instrument
has. Inside its coverage, an answer that lacks a listing in the stated currency
contradicts the key. Outside its coverage, the provider is silent about the listings it
lacks. A provider with limited coverage answers for only some listings, so its answer is
consistent with any stated currency.

Each identity integration declares whether its coverage is complete. OpenFIGI's is
complete. Massive's and EODHD's are limited. The resolver drops a candidate whose
listings lack the stated currency family only when its provider's coverage is complete.
The other checks against the stated key are unchanged: a candidate is still dropped when
it contradicts a stated class or a stated identifier.

OpenFIGI filters its request by the stated currency, so every candidate it returns
carries that currency and passes the check. In M04 every candidate passes the check. It
stays for a complete provider that returns currencies.

## Consequences

A limited provider's candidate that lacks the stated currency survives the check. Below
the winner it attaches only through a stable identifier it shares with the winner, and
its listings join the instrument.

Where the key leaves the currency open, a limited provider's sole listing is a candidate
for a guess rather than an answer. Guesses are deferred; see
[guesses.md](../../../deferred/guesses.md). Until they are built, resolution treats the
answer as it treats any other.
