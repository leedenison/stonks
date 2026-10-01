# Instruments merge through a stable identifier into the earlier

A response whose stable identifiers land on two existing instruments describes one
instrument held twice. The later created is folded into the earlier: its listings move
where the survivor lacks the family and are otherwise relinked onto the survivor's, and
its identifiers, stated keys, fetch keys and coverage follow. The composite foreign keys
onto listings are deferrable so the move is several statements in one transaction. The
merge is recorded as a finding, since it is automatic and irreversible.

The merge is refused where the union would hold two identifiers of one type and domain
with different values. The response then attaches through the instrument its strongest
identifier names, and a contradiction finding records the overlap.

Relinking another user's stated keys happens without that user's key lock. Their
grouping reads the association under the lock later, so one write of theirs may see the
old instrument.
