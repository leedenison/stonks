# Terminology

The terms used in the documentation, the API and the datamodel.

**Source** -- the origin of data the system ingests; a broker statement, a market
datasource, reference data, etc that can state a fact the system records.

**Channel** -- the route data to enter the system with its own authentication,
reliability and correctness properties.

**Datasource** -- a source of market data: eg. prices, corporate events, instrument
identity.

**Authority** -- the trust a source and channel combination carries. **System authority**
is assumed truthful, reliable and correct, **user authority** is not trusted for any of
those properties, and **candidate authority** is a guess.

**Currency Family** -- the currency codes that denote one currency at different unit
scales. GBP and GBp/GBX are one family.

**Venue** -- the market a listing trades on.

**Validity** -- the interval over which an identifier names one instrument.
**Confirmed** where coverage or assertions establish it, **provisional** where it rests
on the assumption that the identifier has not moved.

**Assertion** -- a datasource's claim, made by a fetch, that an identifier names the
instrument its answer describes, holding at the moment of the fetch or over the interval
the answer states.

**Coverage** -- the periods over which one datasource has answered for one key, or for
every key in one domain.

**Integration** -- the code adapting one datasource to the fetch framework: its request
shapes, parsing, venue map and the declaration of what it serves.

**Fetch** -- a run asking one datasource for one kind of data over one period.

**Fetch Key** -- one key inside a fetch, recorded as a row: the identifier sent for it,
the outcome, and the identifiers the answer returned.

**Provenance** -- the fetch key that produced a stored row.

**Finding** -- a row recording something a run met that an administrator may need to see,
referencing the run and the rows it is about.

**Block** -- a record that a datasource failed permanently for a key, suppressing further
calls until an administrator clears it.

**Annotation** -- what a user records against their own stated keys: a pin to an
instrument, a grouping, a price, or a key made by hand. User data, never an assertion.

**Portfolio** -- a defined subset of the holdings of a user including the degenerate
'all holdings' portfolio.

**End of Day Price** -- the closing price of an instrument on a date.

**Corporate Event** -- a change to the terms of an instrument that restates the quantity
held (eg. a split, a reverse split, a stock dividend), or exchanges it for another (eg. a
merger, a spinoff).

**Identifier Event** -- a change to which instrument an identifier names (eg. a ticker
change, a retirement, a reassignment), witnessed by a source, implied by a corporate
event, or inferred from two assertions.

**Event** -- a group of transactions that together form the components of a single
economic event (eg. a stock purchase is an event with component transactions covering
the acquisition of the stock, the payment of the cost, the payment of commission, the
payment of fees and the payment of tax).

**Residual** -- the remainder of an event whose transactions do not balance.
