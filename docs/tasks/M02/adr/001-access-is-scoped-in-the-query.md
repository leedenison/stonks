# Access is scoped in the query

Every query over a user's own data takes the caller's user id and filters on it. Access is
not checked after the read.

A caller with no access gets `not_found`, not `permission_denied`. The two are
distinguishable, and `permission_denied` on an id confirms that the id exists.

## Consequences

Every such query signature carries a user id.
