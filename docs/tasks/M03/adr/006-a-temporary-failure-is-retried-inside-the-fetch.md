# A Temporary Failure is Retried Inside the Fetch

A provider can fail a call for a reason that will not last: a 5xx, a dropped connection, a
timeout. The question is how long that failure suppresses the next call, and where the
interval is held.

No interval is held anywhere. A temporary failure is retried inside the fetch, under an
exponential backoff capped at a small number of attempts, and the whole of it is over in
under a second. What survives the fetch is a block, and a block is cleared by a person
rather than by a clock.

The base interval, the ceiling and the attempt cap are constants in the datasource
package. Which errors are worth repeating and how fast a provider may be called are
provider facts and sit on the integration interface. How much patience the framework has
is a framework fact and belongs in one place.

A call is retried only when the failure is both temporary and about the identifier. A
temporary failure about the datasource, a 429, is not retried: repeating it inside the
second cannot help, and it opens a datasource block at once.

Exhausting the cap promotes the last failure to permanent and opens a block at the scope
that failure carried. See [005](005-a-block-is-on-the-identifier-sent.md).

## Consequences

A datasource having a bad hour costs up to the cap in calls per key, which nothing stops
short of the administrator clearing what it blocked.

An identifier whose retries were exhausted is now blocked, so a replay re-trying the keys
whose identification was unavailable would skip it. The block names the fetch key that
provoked it, and that key's outcome distinguishes a datasource that refused the identifier
from one that never answered, so replay can be given the second kind to clear without any
new row. Which of them it clears is issue
[009](../issues/009-replay.md)'s to settle.
