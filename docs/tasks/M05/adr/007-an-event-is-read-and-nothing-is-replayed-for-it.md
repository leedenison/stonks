# An event is read, and nothing is replayed for it

An identifier event is a fact about a value. Everything that rests on the value reads
it at the next read: a dated key's transactions take the instrument the value's validity
names on their dates, as [013](013-an-association-is-fixed-or-dated.md) and
[014](014-a-transactions-instrument-is-derived-when-read.md) state, and the statement
page marks them from the same derivation. A fixed association never rests on the value,
so an event never touches it. No key is replayed because an event arrived, and nothing
on a key records an assumption for an event to find.

## Considered options

A record for each assumption an association makes, naming the run, the value, the
interval and the key, so that an event could find and unwind it. With nothing stored
per key, there is nothing to unwind.
