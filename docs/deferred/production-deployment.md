---
title: Production deployment
recorded: 2026-09-10
---

# Production deployment

The stack as it is served to real users: TLS at the edge, the origins the API admits, and
what each container exposes to its network.

## Why

The edge speaks plain HTTP on one origin. The session cookie therefore travels without the
Secure flag, and where a front end is served from anywhere else, it cannot call the API.

Every stack publishes Postgres, Redis, the service's own port and the Envoy admin interface
on every interface of the host. Postgres holds every user row and Redis every live session,
so a reachable port is a reachable account. The admin interface serves a configuration dump
and accepts a shutdown request. Postgres takes a password equal to its user name and Redis
takes none, which is the whole of what stands in front of either.

## Sketch

TLS on the listener, with a certificate the operator supplies or an ACME sidecar renews,
and HSTS once it is on. STONKS_COOKIE_SECURE is then left at its default in every stack
that is reached over TLS.

A CORS policy on the API route only, naming each allowed origin explicitly and allowing
credentials. Never a wildcard, which a browser refuses to pair with a cookie.

The edge listener is the only port published to every interface. Every other service is
reached over the compose network by name, and where a developer wants a port on the host,
it is published on the loopback interface. The Envoy admin interface is bound to loopback
or left unpublished.

A Postgres password and a Redis password generated per deployment and supplied through the
environment, so that no credential is a literal in a compose file. The service reads both
from its connection URLs, which is where it already reads them.

A gate on known vulnerabilities in what is deployed: govulncheck over the server tree and
an audit of each npm project, reporting against the pinned versions from which the
images are built.

## Undecided

The source of the certificate. Whether a second origin is ever wanted, since the
application is served from the same edge as its API. Whether the development stack keeps
published data store ports for the convenience of pointing a client at them, and whether
loopback bounds them sufficiently if it does. Where a deployment keeps its secrets.

Whether the vulnerability gate blocks a pull request or runs on a schedule. When an
advisory is published against a dependency, it fails a branch that did not touch the
dependency, which argues for a schedule; a dependency the branch under review introduces
argues for the gate.
