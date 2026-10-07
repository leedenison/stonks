# EODHD spends three calls on an identity answer

EODHD's search returns the listings an identifier names at one call. It omits the FIGI,
and it names every US venue by the one code US. An answer from search alone corroborates
an OpenFIGI answer only through a stated ISIN, and leaves the venue of a US listing
unstated. The integration therefore also calls id-mapping, which returns the composite
FIGI of a symbol, and the exchange symbol list, which returns the venue of a US symbol.
Each costs one call, so an identity answer costs up to three against a daily quota of a
hundred thousand on a paid plan.

The calls buy corroboration and a venue. With the composite FIGI an EODHD candidate for a
key that states a ticker attaches to the OpenFIGI answer. With the venue a US listing
states a MIC_TICKER, and resolution matches a key that states a ticker at a venue through
that identifier.
