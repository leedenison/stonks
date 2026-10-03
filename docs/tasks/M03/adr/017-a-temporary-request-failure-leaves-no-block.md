# A temporary request failure leaves no block

A request refused for its rate or for the provider's health is retried inside the fetch,
and when it outlasts the retries the fetch has to say what becomes of the keys it
carried. Blocking each of them treats a provider's bad hour as a fact about the
identifiers, and leaves an administrator a block per key to clear by hand before any of
them can be asked again.

A temporary failure writes no block. The keys are recorded as failed temporarily, the
resolution leaves them unavailable, and a replay over the unavailable keys asks for them
again. A permanent failure still blocks at the scope the integration gives it: the whole
datasource, or each identifier the request named.

## Consequences

A temporary failure the integration classifies as about the datasource also leaves no
block, so the remaining chunks of that fetch each cost one unretried call. OpenFIGI
classifies no temporary failure that way; a rate limit or an outage is about the
identifiers it carried.
