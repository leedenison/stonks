# Terminology

The terms used in the documentation, the API and the datamodel.

**Source** -- the origin of data the system ingests; a broker statement, a datasource,
reference data, etc that can state a fact the system records.

**Channel** -- the route data to enter the system with its own authentication,
reliability and correctness properties.

**Datasource** -- a source of market data: eg. prices, corporate actions, instrument
identity.

**Authority** -- the trust a source and channel combination carries. **System authority**
is assumed truthful, reliable and correct, **user authority** is not trusted for any of
those properties, and **candidate authority** is a guess.

**Currency Family** -- the currency codes that denote one currency at different unit
scales. GBP and GBp/GBX are one family.

**Venue** -- the exchange or trading system where a listing trades.

**Coverage** -- the periods over which one datasource has answered for one key, or for
every key in one market.

**Provenance** -- the fetch key that produced a stored row.

**Annotation** -- what a user records against their own stated keys: a choice of
instrument, a grouping, a price, or a key made by hand. User data, never an assertion.

**Portfolio** -- a defined subset of the holdings of a user including the degenerate
'all holdings' portfolio.

**End of Day Price** -- the closing price of an instrument on a date.

**Identifier Event** -- a change to which instrument a ticker names (eg. a ticker
change, a retirement, a reassignment).

**Event** -- a group of transactions that together form the components of a single
economic event (eg. a stock purchase is an event with component transactions covering
the acquisition of the stock, the payment of the cost, the payment of commission, the
payment of fees and the payment of tax).

**Residual** -- the remainder of an event whose transactions do not balance.
