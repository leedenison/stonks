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

A fact is keyed by a market ticker: a market and a ticker's value. It is a type of its
own, and a MIC_TICKER never carries a market. In `identifier_assertions` a `mic_ticker`
row holds its market in `domain`. The same column holds the domain of the ISIN, FIGI and
datasource ticker rows. The event and coverage tables hold tickers only, and name the
column `market`.

Market membership is reference data. A table assigns an operating MIC to a shared market.
An operating MIC the table omits is a market of its own. A shared market has a code of its
own, such as `US`. A market of one operating MIC is named by that MIC.

A datasource answers for a market whole or not at all. An identifier events integration
states facts for one market. It covers every operating MIC of that market, and refuses to
build otherwise. A negative assertion or a coverage row is then a claim about the whole
market. The identity integrations meet this for the US. Massive and EODHD look a ticker up
nationally. OpenFIGI searches every exchange.

OTC symbols share the US namespace, and EODHD's `US` code reaches them. The OTC operating
MICs are outside the US market. Massive's venue table leaves them out, and Massive must
cover its market whole.

When two answers state one ticker in one market on one UTC day, they corroborate. So
`BATS:FB` and `XNAS:FB` join. Extending validity matches a held ticker to a listing at any
venue of its market. For example, a key at `XNYS:FOO` takes an answer that lists
`XNAS:FOO`.

## Considered options

- Keep facts per operating MIC. Rejected: it multiplies the rows and the attestations
  by the venues of a market, and a listing transfer reads as a ticker that retired at one
  venue and appeared at another.
- Make the market the domain of a MIC_TICKER. Rejected: listings, their names and the
  keys a broker states all carry the venue, and the venue picks a listing's primary.
- Key facts by operating MIC and read the union of a market's MICs. Rejected: it still
  needs the market to read, and a listing transfer still splits the history.
