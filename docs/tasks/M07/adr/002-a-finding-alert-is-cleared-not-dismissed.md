# A finding alert is cleared, not dismissed

Findings carry `cleared_at`, and `ClearFinding` sets it. The findings migration
distinguishes an occurrence, which belongs to the run that raised it, from a standing
finding, which reports a condition of the data and keeps its row when cleared.

The finding alert kinds are contradiction, identifier disagreement, broken ticker
validity bracket, missed event and unmatched split. Each records a choice the system
made between sources, or a mismatch it found, and an administrator checks it. A finding
alert is open while its finding is uncleared. Clearing the finding is the
acknowledgement, and the page keeps its state in the finding. When an administrator clears an
occurrence, a later run that repeats the choice raises a new row and a new alert. The
unmatched split is standing and stays cleared.

Dropped and merged findings are routine on most runs and stay on the run page. For a
block, the open block is the alert. The unhandled corporate
action finding is the subject of the corporate action alert, but that alert is open while
the action is undecided, and only a decision settles it. `ClearFinding` refuses the kind
as it refuses a block finding. See [006](006-a-corp-act-decision-is-a-type-on-the-action-row.md).
