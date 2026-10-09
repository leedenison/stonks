# A listing gathers the venues every provider names

A listing is one currency family of an instrument, and its venues are fungible. Providers
state a listing at different grains. OpenFIGI answers per venue and per composite
exchange code. EODHD answers at a composite code such as US, and its exchange symbol list
names the venue. Massive answers at the primary exchange only.

An answer that states a listing at a composite merges with answers that state it at a
venue. The merge goes through the stable identifiers they share, the composite FIGI or
the ISIN, and through the currency family. Each answer adds a MIC_TICKER for every venue
it names. An answer that names only a composite adds its own ticker types. Over time a
listing gathers a MIC_TICKER at each venue that some provider named.

The listing does not record the venue of a transaction. The MIC_TICKERs are handles for a
source that must be asked at one venue, such as a price source that needs a venue to tell
two symbols apart. A listing may record the venue a datasource states as its primary.
The primary venue names the listing to the user. Which tickers associate a key does not
depend on it.
