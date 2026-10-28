# An identifier assertion holds a period

An identifier assertion claims what a ticker names on one day. A nullable `edge` marks
that day as the first or last of a span. A Massive ticker history states a link of its
chain as one period, but it is stored as a `first`, a `last` on the day before the cut,
and a point on today. The days between are never asserted. A weaker source's assertion
inside the link therefore wins its day, and derivation infers two changes that did not
happen.

A datasource that answers twice for one ticker and day keeps both assertions. Precedence
cannot order them, so the lower instrument id wins. A correction or a retirement from the
datasource never takes effect, and the datasource raises `identifier_disagreement`
against itself.

## Decision

An identifier assertion holds a half-open period `[asserted_from, asserted_before)`. Each
bound is an edge or open. An edge is a change the source states at the start of its day.
Every day of the period is asserted, and competes with the assertions of other sources.

| Fact | Assertion |
|---|---|
| A lookup on day D | `[D, D+1)`, both bounds open |
| A link of a ticker history | `[from, before)`, an edge at each change |
| An identifier attestation from D1 to D2 | `[D1, D2+1)`, both bounds open |

Of two answers from one source, the later wins. A source is a datasource. All identifier
attestations together count as one source. This applies the principle of
[001](001-a-corporate-action-is-stored-against-its-ticker-and-attributed-through-ticker-validity.md)
to identifier facts.

- The reach of an answer is the periods of its assertions. The reach of an answer of
  identifier facts also holds the period of its request, on each ticker it states. A
  ticker history therefore replaces the earlier history of each ticker it states.
- An earlier answer keeps only the days outside the reach of later answers from the
  same source.
- An earlier answer loses an edge when the reach of later answers from the same source
  holds both days around it.
- Answers are ordered by their moment, then by their id. The moment of a fetch key is its
  `answered_at`, and a cached answer keeps the moment of the call that filled the cache.
  The moment of an identifier attestation is the creation of its run.

`identifier_period_assertions` becomes `identifier_assertion_coverage`.

## Considered options

- Replace the whole earlier answer, as corporate actions do. One datasource's identifier
  answers hold different periods. A lookup and a ticker history cannot replace each other
  whole.
- Limit the reach of an answer to the periods of its assertions. A ticker history that
  moves a rename later would then keep the earlier history's days before the new rename.
  The new history's edge would cut them from the new history, and the datasource would
  break its own bracket.
- Delete superseded assertions as they are written. The admin Tickers tab would lose the
  answers it shows.
- Store points and edges in separate tables. An edge is always a claim about the period
  of its assertion.
