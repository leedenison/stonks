# The service speaks Connect over plain HTTP

A browser client and a Go service exchange protobuf over HTTP using connect. Envoy
terminates TLS, serves CORS, and routes the API to the service. Connect generates ordinary
`net/http` handlers serving the Connect, gRPC and gRPC-Web protocols on one endpoint.
Cookies are then ordinary HTTP headers in both directions with no translation layer.

Envoy routes non-API requests to the front end.  It allows front end origins explicitly
and with credentials rather than `*`, and forwards `Cookie` and `Set-Cookie` untouched.

## Consequences

The browser client uses `@connectrpc/connect-web`, which use the service descriptors
`protoc-gen-es` emits.
