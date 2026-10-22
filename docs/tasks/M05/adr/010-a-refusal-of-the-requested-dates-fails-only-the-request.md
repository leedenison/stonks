# A refusal of the requested dates fails only the request

A provider may refuse a request because its dates lie outside what the subscription or
the endpoint holds. Massive answers a 403 for a date outside the plan's history window.
The refusal does not concern the identifier. A request for other dates under the same
identifier may succeed.

Such a refusal fails the request permanently and does not write a block. An integration
gives its failure one of three scopes:

- The key. The failure blocks the identifier or domain that the key went out as.
- The datasource. The failure blocks the datasource.
- The request. The failure leaves every identifier and the datasource unblocked.

The key is the default scope. An integration that does not name a scope keeps the
behaviour it has today.

## Considered options

- An identifier block. It would refuse every later period under the identifier, including
  periods inside the window.
- A temporary failure. The framework would repeat a call that cannot succeed, and a replay
  would repeat it again.
- A new `block_scope` value. Every block row would still carry another scope, so the value
  would go unused.
