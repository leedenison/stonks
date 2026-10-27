# A stronger source overrules an edge

`tickervalidity.go` and `006_findings.sql` say of an identifier event between two
assertions of one instrument that "the event wins over the bracket". Precedence decides
only within a day. A weak source's identifier event therefore cuts a strong source's
bracket. Under [009](009-an-identifier-event-is-recorded-as-assertions-with-edges.md) the
weak source's edge assertion can also win its own day. The winners then differ on either
side of it. The change looks real, and the system raises no finding. An identifier
attestation cannot remove the edge either. Attesting the edge's day wins that day. The cut
still applies.

A source outranks another when it is an identifier attestation, or when its precedence
number is lower. A stronger source overrules an edge in two ways:

- **A bracket.** The stronger source's nearest assertions on either side of the cut name
  the same instrument.
- **A period.** A period assertion of the stronger source holds the days on both sides of
  the cut. The source does not report an edge at the cut.

An overruled edge does not cut the timeline. Its assertion still competes for its day as
a point assertion. It raises a new `overruled_edge` finding, which names the fetch key
that supplied the edge.

An edge that cuts across a bracket of an equal or weaker source raises
`broken_ticker_validity_bracket`, which keeps its meaning.

An administrator can overrule a wrong edge by attesting one instrument on the days around
it.

## Considered options

- Every edge cuts. A weak source then overrules a strong one without a finding. An
  identifier attestation cannot correct it. The only remedy is deleting the fetch key.
- Every edge cuts, and an edge inside a stronger bracket raises a finding. The
  administrator sees the case. An identifier attestation cannot correct it.
