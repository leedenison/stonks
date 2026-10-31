# A corporate action decision is a type on the action row

A corporate action applies to a holding only when it restates the share count. The
datasource's type and the ratio test cannot settle that alone, so an administrator
decides. The decision is a type: split, reverse split, stock dividend, spin-off price
factor, rights issue or other. Whether the action restates shares follows from the type:
the first three restate, the rest do not. A type keeps what D-CORP needs to apply the
action in full later.

The decision is a nullable column on the `corp_acts` row, where NULL means undecided.
The effective restates-shares of an action is its decision where one exists and its
stored `handled` otherwise. The decision overrules the datasource and the ratio test in
either direction, and an administrator can change it from the instrument's corporate
actions tab.

A refetch pairs stored actions with reported ones and keeps each paired row. The
pairing ignores the decision, so the decision survives. A refetch that restates the ex-date or ratio
deletes the row, the finding that names it and the decision. The restated action is a
new action and raises a new alert.

The corporate action alert is open while the action is unhandled and undecided. The
decision settles it. Clearing the finding alone does not, and `ClearFinding` refuses the
kind. This answers the D-RUNS Undecided item on clearing an unhandled corporate action;
the close issue amends the note.

Holdings are restated on read from the instrument's calendar, so a changed decision takes
effect at once and nothing is invalidated.
