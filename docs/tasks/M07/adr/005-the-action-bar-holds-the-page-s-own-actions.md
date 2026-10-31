# The action bar holds the page's own actions

PR 385 put the system runs in two menus of the runs page action bar. A bar of menus
hides what each operation does and whether it is running, and it fills as operations are
added.

The action bar of a page holds at most one primary action and the text actions that act
on the page's record. A catalogue of operations is a table in the page body, one row per
operation, with its name, a sentence on what it does, its state and its button. A table
scales by rows and shows state. A menu holds the choices of one control, such as a
filter, never a list of operations.

The system runs are such a catalogue. They leave the runs page bar and become a Runs card
on the alerts page. The convention is written into the frontend-design skill at close,
with the user's explicit agreement.
