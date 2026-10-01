# A composite establishes venue equivalence

Venues are fungible within a listing, and a listing is one currency family of an
instrument; see [003_instruments.sql](../../../../server/internal/migrations/003_instruments.sql).
The evidence that two venue
tickers are one listing is the OpenFIGI composite FIGI, which links the trading venue
FIGIs of one instrument within one country or market. Where candidates share a composite,
they describe one listing. The composite is stored on the listing as a global listing
grain identifier, and a MIC_TICKER is stored for each venue of the composite as a handle
for a datasource that must be asked at one venue, such as a price query naming an
exchange.

The composite is not the listing's key. One currency has several composites of one
instrument: a multilateral trading facility such as Tradegate carries its own composite
rather than the national one, and a Eurozone cross-listing is one composite per country,
all in EUR. Those are one listing here, since holdings sum per instrument and a price from
any of them serves. Where a candidate is answered without a composite, or under a
composite exchange code naming no venue, it can only be grouped by currency, so the family
has to be the unit regardless.

A composite is taken to be in one currency. The FIGI allocation rules do not say so. It
follows from a composite being one ticker in one market and a Bloomberg ticker naming one
currency line: a second currency on the same exchange is a second ticker and a second
composite, AAPLEUR beside AAPL, and London International beside London. The recorded
cassettes bear it out, with no composite answered having members under more than one
ticker. Where a key states a currency the call filters on it, so the assumption carries
weight only for a key stating none.

## Consequences

The OpenFIGI integration filters a MIC_TICKER on its ticker alone and answers every venue,
recording the ticker without its venue as the filter. Choosing the stated venue's
composite is resolution's; see [resolve.go](../../../../server/internal/resolve/resolve.go).
So a venue lacking an exchange code of its own, or a statement naming the wrong venue,
leaves the choice to ranking instead of making the key unrecognised.

A listing carries one composite per market where it trades. An identifier still names one
listing.
