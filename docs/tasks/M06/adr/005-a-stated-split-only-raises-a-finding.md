# A stated split only raises a finding

IBKR states a split as a transfer of the units it added, with a ratio. Schwab restates
its rows to the export date and states a split as the shares added, without a ratio.
Each marshaller must recognise these rows anyway, or holdings would count the split
twice.

A stated split is never a source of corporate events. IBKR dates a split on the last
trading day before its ex-date, and Schwab omits the ratio. Each stated split is kept as
a check on the calendar. It is the only evidence independent of the providers that a
covered period lacks a corporate event.

The corporate event fetch run checks each stated split of a key whose instrument it now
covers on the split's date. A dated key reaches its instrument through validity, and a
fixed key names it. The split matches when the calendar holds a corporate event for the
same instrument within 7 calendar days either side, with an equal ratio where the
statement states one. Otherwise the run records an `unmatched_split` finding that
references the stated key. The window counts calendar days because the system does not
hold a trading calendar. A stated split on an option is skipped.
