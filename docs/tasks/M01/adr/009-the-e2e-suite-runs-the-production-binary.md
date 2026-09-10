# The e2e suite runs the production binary

The e2e suite needs the stack to behave predictably: an identity provider that always
verifies, and later a price provider that always answers the same way. The usual way to get
that is a test-only build -- a build tag that registers an extra service and swaps real HTTP
clients for recorded ones, driven by RPCs the suite calls between specs.

Stonks does not do this. The binary under test is the binary that ships, and the only
difference between the e2e stack and any other is configuration.

Testing a different artifact from the one released undermines the tier that exists
specifically to check the whole thing fits together, and it does so in the layer hardest to
reason about. A test-only service also has to be exempted from authentication, which puts a
hole shaped like a test fixture in the specification of production authorization: the
interceptor's policy would list an exemption for something that is supposed not to exist in
production. And the exemption is load-bearing on a single build flag with nothing behind it
at runtime.

## Considered options

**A runtime guard instead of a build tag** keeps one artifact, which is the property being
protected, but leaves a process-restarting unauthenticated endpoint in the shipped binary
behind a configuration check. For a destructive capability that is worse, not better.

**Per-suite recorded HTTP, swapped in-process**, is what a test service would mostly be for.
It is rejected with the service: e2e needs a provider to answer *deterministically*, not
*authentically*, so a recording buys nothing there that a stub does not.

## Consequences

External traffic in e2e is seeded as a result, or answered by a stub at the network
boundary that the server reaches by configuration. Recorded traffic belongs to the
integration tier, where the client code under test is the thing that has to handle what the
provider really sends.

State a test needs to observe is exposed on the real API. If a test wants it, an operator
usually wants it too, so it is built as a feature with a supported read path rather than as
a backdoor.

Environment-level disruption -- stopping a container, severing a network -- is the way to
reach failure and restart paths, because it exercises the real startup path rather than a
hook that only exists under test.
