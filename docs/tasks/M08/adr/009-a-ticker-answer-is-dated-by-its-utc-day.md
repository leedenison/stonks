# A ticker answer is dated by its UTC day

The day of a ticker answer is the UTC day of the moment of the answer. Two answers fall
on one trading day when they share that day.

## The local date of the market

The alternative reads the moment in the time zone of the market. It needs a zone for each
market, seeded from the ISO 10383 list, with a table kept by hand for each country that
has several zones. The gain is too small to pay for that:

* Answers that corroborate each other come from one run, seconds apart, or from the fetch
  cache within its lifetime of one hour. A wrong join needs the two answers either side
  of the midnight of the market, and a reassignment of the ticker at the open between
  them.
* An answer between UTC midnight and New York midnight is asserted for a New York day
  that has not started. The assertion is wrong only where the ticker is reassigned at
  the next open.
* The local date is itself an approximation of the trading day. US venues plan overnight
  sessions that date a trade after 21:00 by the next calendar day.
* Massive is the only identifier event source and it covers the US market. The zone of
  every other market would change nothing.

A missed join costs a second fetch. A later answer corrects an assertion or a coverage
period that an earlier answer got wrong.

## The moment of an answer

The moment of an answer stands for the day its data describes. A datasource that updates
its ticker map in an overnight batch would report the assignment of the day before. The
datasources on hand are not known to lag. The harm needs a reassignment inside the lag.
The risk is accepted. If a datasource is found to lag, its client states the day its
answer describes, as a client states the "as at" date of a price.

When the system reads an answer from the fetch cache, the answer keeps the moment at
which the datasource answered.

## The last completed day

The last completed day of a listing is the UTC day before today. Every market has closed
its regular session for that day by then. A market east of UTC waits some hours past its
close for the UTC midnight. See [005](005-the-days-held-are-demanded.md).
