# An association records its arbiter

A stated key records the arbiter of its association beside its validity: stated,
datasource, guess or user. It is set whenever the instrument is set and cleared with it.
Resolution writes stated or datasource, or user where the key inherits a confirmation; see
[012](012-a-confirmation-carries-forward-through-a-shared-identifier.md). A confirmed
candidate writes user. Nothing writes guess until guesses are built.

Confirming a candidate for one key confirms it for every key in that key's group, the
unresolved keys that share an identifier. Monthly statements each carry their own key
for one holding, so the user confirms the holding once.

Resolution leaves an association in place when the user arbitrated it. A replay skips a key
whose arbiter is user when it selects keys, and the resolver's write refuses to change
the association of such a key. A merge of two instruments still moves the key to the
surviving instrument, since the key names the same instrument.
