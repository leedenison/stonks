# Terminology

The terms used in the documentation, the API and the datamodel.

**Source** -- the origin of data the system ingests; a broker statement, a datasource,
reference data, etc that can state a fact the system records.

**Channel** -- the route data to enter the system with its own authentication,
reliability and correctness properties.

**Datasource** -- a source of market data: eg. prices, corporate events, instrument
identity.

**Authority** -- the trust a source and channel combination carries. **System authority**
is assumed truthful, reliable and correct, **user authority** is not trusted for any of
those properties, and **candidate authority** is a guess.

**Currency Family** -- the currency codes that denote one currency at different unit
scales. GBP and GBp/GBX are one family.

**Venue** -- the exchange or trading system where a listing trades.

**Validity** -- the interval over which an identifier names one instrument.
**Confirmed** where coverage or assertions establish it, **provisional** where it rests
on the assumption that the identifier has not moved.

**Association** -- what a stated key resolved to. **Fixed** where it names one instrument
for all time, **dated** where it names a MIC-derived value and each transaction takes the
instrument the value names on its date.

**Assertion** -- a claim that an identifier names an instrument, or names nothing, on one
day. A datasource makes one through a fetch, and it holds on the day of the answer. A
point in time lookup holds on the day its request names. An entity's ticker history
asserts each ticker on the day the ticker began naming the entity. An administrator makes
one by attestation.

**Coverage** -- the periods over which one datasource has answered for one key, or for
every key in one domain.

**Provenance** -- the fetch key that produced a stored row.

**Annotation** -- what a user records against their own stated keys: a choice of
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
