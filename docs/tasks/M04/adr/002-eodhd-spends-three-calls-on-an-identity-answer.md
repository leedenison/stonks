# EODHD spends three calls on an identity answer

EODHD's search returns the listings an identifier names at one call. It omits the FIGI,
and it names every US venue by the one code US. An answer from search alone corroborates
an OpenFIGI answer only through a stated ISIN, and leaves the venue of a US listing
unstated. The integration therefore also calls id-mapping, which returns the CUSIP of a
US symbol, and the exchange symbol list, which returns the venue of a US symbol.
Each costs one call, so an identity answer costs up to three against a daily quota of a
hundred thousand on a paid plan. A key that states only a CUSIP or composite FIGI costs a
fourth, since id-mapping first finds the ISIN that search takes.

The calls buy a CUSIP and a venue. With the venue a US listing states a MIC_TICKER, and
resolution matches a key that states a ticker at a venue through that identifier.

Id-mapping also returns a FIGI. For some symbols it is the composite FIGI and for others
an exchange's FIGI. BBG000DWH8Q0, for BRK-B.US, is an exchange's FIGI. The integration
does not read it, so an EODHD candidate for a key that states a ticker shares no stable
identifier with an OpenFIGI answer.
