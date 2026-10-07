# Each provider maps its venues through a generated table

Each integration holds a table of the provider's venue codes, generated from the
provider's own list, as the OpenFIGI integration does. The table is a Go map in the
integration's package, since only the integration reads the provider's codes. A script in
`local/scripts` regenerates it, and the generated file is committed. The generator drops a
code when the mics table lacks its MIC, so every MIC the table holds normalises to an
operating MIC.

EODHD has two tables:

- Its exchange codes, from `/api/exchanges-list`, which publishes the operating MICs of
  each code. A code spanning several venues, such as US, maps to all of them, and names a
  venue only through the second table. The integration also maps an operating MIC back to
  its code, to filter a search by the venue a key states.
- The venue names its exchange symbol list gives a US symbol, such as NYSE, NASDAQ and
  BATS. EODHD does not publish a MIC for these, so the table is written by hand from the
  names the list returns.

Massive states its primary exchange as a MIC, so its table only lists the venues
Massive covers, from `/stocks/v1/exchanges`, and the integration serves a ticker
only at a venue in it.
