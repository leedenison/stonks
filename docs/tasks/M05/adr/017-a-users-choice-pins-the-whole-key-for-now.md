# A user's choice pins the whole key, for now

A user's choice of an instrument for a key gives the key a fixed association with that
instrument, with the user as arbiter. It is not checked against validity, and it holds
for every transaction of the key, whatever their dates. A later event on the value the
key states does not touch it.

This is an interim rule. A choice carries one of two intents that the system should
tell apart: the user overrides what a datasource said because they think it is wrong, or
the user fills in a blank that no datasource could provide. The two interact differently
with validity, with later coverage and with an event. Issue
[011](../issues/011-a-users-choice-overrides-or-fills-in.md) settles that and replaces
the pin.

The pin is taken now because it is what a choice does already, and because a withheld or
unresolved transaction needs a way out in this milestone.
