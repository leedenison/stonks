# A Block is on the Identifier Sent

A datasource that refuses a key it declared it serves will refuse it again, and the calls
that repeat the refusal cost quota. A block records the refusal and suppresses them until
an administrator clears it.

A block is keyed on the identifier the call was sent under, not on the key being answered.
The provider refused the identifier; the identifier belongs to nobody, so one user's
refusal stops every user's call. A stated key would be the wrong subject twice over: it
belongs to one user, and it belongs to one statement, so re-uploading that statement mints
a new key and bypasses the block.

A block carries a scope, which the integration's classification of the failure decides.
A failure about the identifier -- the provider could not answer for this one value --
blocks that identifier. A failure about the API key, a spent quota or a rejected
credential, blocks the datasource. Only the integration knows which the provider's codes
mean.

A block names the fetch key that provoked it, which is an administrator's record of what
happened and is how the reason is traced back to a call.

## Consequences

A datasource-scoped block suppresses one kind of data, not the row entirely, since a
provider may sell identity and prices against separate quotas.

Blocks are read once at the start of a fetch, so a block written by one fetch takes effect
on fetches that start after it and a concurrent fetch finishes its key set. Closing that
window would cost a query per key.
