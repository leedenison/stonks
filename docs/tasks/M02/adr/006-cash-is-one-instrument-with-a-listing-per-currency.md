# Cash is one instrument with a listing per currency

Cash is a single system owned instrument of asset class CASH, seeded with one listing per
supported currency. Each listing holds a listing grain identifier of type CURRENCY, with
no domain and the ISO 4217 code as its value.

A marshaller states that identifier in the stated key of a cash leg, so the leg resolves
to the listing for its currency without a datasource and without creating anything. A
cash holding is then the sum of a user's transaction quantities against one listing.

## Consequences

The cash instrument, its listings and their identifiers are the only system owned rows
until a datasource exists, and the seed is a migration rather than an upload.
