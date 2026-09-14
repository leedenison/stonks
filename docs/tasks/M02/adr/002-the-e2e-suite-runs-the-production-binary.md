# The e2e suite runs the production binary

The binary under test is the binary that ships, and the only difference between the e2e
stack and any other is configuration.

## Consequences

External traffic in e2e is seeded as a result, or answered by a stub at the network
boundary that the server reaches by configuration.

State a test needs to observe is exposed on the real API. If a test wants it, an operator
usually wants it too, so it is built as a feature with a supported read path rather than as
a backdoor.

Environment-level disruption -- stopping a container, severing a network -- is the way to
reach failure and restart paths, because it exercises the real startup path rather than a
hook that only exists under test.
