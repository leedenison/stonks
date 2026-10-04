# The e2e provider is a replaying proxy

The e2e suite drives the shipped binary, so its provider has to be a service the
datasources row names. A stub answering from a hand-written corpus would assert how the
provider responds, and could be wrong or drift. The stand-in is instead a reverse proxy
whose transport is go-vcr: `make e2e-record` forwards what the recording lacks to the
provider and appends what comes back, and every other run replays. The provider authors
every byte the binary sees in e2e, and the integration tier keeps its own cassettes for
what the parser makes of them.

One case cannot be recorded: a key refused for its rate and then served, which the replay
spec needs. Those refusals live in an authored cassette the proxy consults before the
recording, once per interaction in order. It is the only unrecorded traffic in the suite
and is kept in a file of its own, so recorded bytes are never edited.

## Consequences

Fixtures state real public identifiers where a listing is wanted, each spec a different
security, so no spec's lookup finds an instrument another spec's fetch created. A seeded
instrument that no spec fetches carries invented identifiers, so no recorded answer can
merge onto it. Any fixture that draws a provider answer states real public identifiers.
A change to how the client builds a request invalidates the recording as a whole, and
every spec fails with a 502 naming the request until `make e2e-record` is run again.
