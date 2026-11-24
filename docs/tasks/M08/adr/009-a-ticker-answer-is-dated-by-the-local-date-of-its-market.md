# A ticker answer is dated by the local date of its market

The day of a ticker answer is the moment of the answer read in the time zone of its
market. Two answers fall on one trading day when they share that date.

## The test

The alternative test joins two answers that fall between the same two sessions, which
would also join a Saturday answer with a Sunday one. It needs the holidays and half days
of every venue, kept current, and the datasources on hand do not serve them. Answers that
corroborate each other mostly come from one run, seconds apart, so the extra joins are
rare. A missed join costs a second fetch, while a wrong join names the wrong instrument.

## The time zone

A market has one time zone. The US market is the only one that groups several operating
MICs, and its venues trade one national session on New York time. Every other market is
one operating MIC, and the zone follows the operating MIC of a segment.

The zones are seeded from the ISO 10383 list, which states a country and a city for each
MIC:

* A market in a country that has one time zone takes that zone.
* A market in a country that has several is placed by its city.
* A market that neither rule places reads as UTC.

## The moment of an answer

The moment of an answer stands for the day its data describes. A datasource that updates
its ticker map in an overnight batch would report the assignment of the day before. The
datasources on hand are not known to lag. The harm needs a reassignment inside the lag.
The risk is accepted. If a datasource is found to lag, its client states the day its
answer describes, as a client states the "as at" date of a price.

When the system reads an answer from the fetch cache, the answer keeps the moment at
which the datasource answered.

## Consequences

* Ticker validity periods and assertions already hold dates, and those dates are days of
  the market. The dates of two markets are never compared, because a ticker belongs to
  one market. `types.Period` keeps its shape.
* The same zone gives the last completed day of a listing. See
  [005](005-a-fetch-covers-the-days-held.md).
