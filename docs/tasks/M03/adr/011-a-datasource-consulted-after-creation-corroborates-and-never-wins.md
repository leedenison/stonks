# A datasource consulted after creation corroborates and never wins

An instrument is created from the response of the highest precedence datasource that
offered a group surviving the drops, and that response supplies its metadata. A
datasource enabled later, or one that had not covered the instrument when it was
created, is still requested for the next key that resolves to the instrument. Its
response corroborates: it adds the identifiers of the group that shares a stable
identifier with the instrument and fills the listing the stated family lacks. It never
replaces what the instrument holds, never changes the class, and raises a contradiction
finding where it disagrees. The outcome therefore depends on the order datasources were
enabled, and no instrument is re-resolved when the registry changes.

Which datasources are still asked is decided by identity coverage, one row per
instrument and datasource, written by the resolution from every served fetch of a key
that matched the instrument, whether or not the response attached anything. A served
response for the identifier that names the instrument is the datasource's answer for it.

A dropped candidate is not stored as an instrument: it is held by nobody, would be shared
by everybody, and was not the response to a strictly filtered call.
