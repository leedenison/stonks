# A ticker's facts are keyed by its market

A MIC_TICKER's domain is its operating MIC. Identifier facts, the assertions, events and
coverage a datasource reports and the attestations an administrator writes, are keyed by
that domain. A US symbol is unique across every US listing exchange, so Massive answers
for the national symbol. Its integration states each fact once for each of the seven
operating MICs its venue table maps to, seven times for one chain answer. An attestation
of a US ticker covers one of those seven. A listing that moves from NYSE to Nasdaq keeps
its symbol, and its history splits across two domains.

A market is the namespace of a ticker's facts. Each operating MIC belongs to one market.
The US venues Massive lists form one market. Elsewhere a market is its operating MIC, since
one symbol at XLON and at XETR may name different instruments. The fact tables and
attestations are keyed by market. Identifier rows, listings and stated keys keep the
operating MIC, because a listing trades at a venue and a broker states a venue. Validity
is read at the ticker's market. A datasource that answers for a market writes each fact
once.

## Considered options

- Keep facts per operating MIC. Rejected: it multiplies the rows and the attestations
  by the venues of a market, and a listing transfer reads as a ticker that retired at one
  venue and appeared at another.
- Make the market the domain of a MIC_TICKER. Rejected: listings, their names and the
  keys a broker states all carry the venue, and the venue picks a listing's primary.
