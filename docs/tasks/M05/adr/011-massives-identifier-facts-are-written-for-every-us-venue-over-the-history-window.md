# Massive's identifier facts are written for every US venue over the history window

Massive's ticker events do not name a venue. Massive answers at composite level, where
one symbol is unique across the US consolidated tape. A listing that moves between venues
and keeps its symbol does not appear as an event there.

Massive's identifier facts are the events, assertions and coverage that its replies
state. Each is written once for each operating MIC that Massive lists. EODHD's domain
coverage takes the same form. A ticker goes out without its venue, so one cache key and
one block serve every venue.

Identity is unchanged. It asserts a ticker at the primary exchange and at the MIC the
request names.

Every chain request asks over the history window: from the first day the subscription
allows to the present. The chain call does not accept a date, so the period only clips
the identifier facts. One period for every caller gives one cache key per entity per day,
whichever of its tickers led to the fetch. The period is longer than the earliest
transaction of a key needs. Coverage beyond that transaction costs nothing, since the call
is the same.
