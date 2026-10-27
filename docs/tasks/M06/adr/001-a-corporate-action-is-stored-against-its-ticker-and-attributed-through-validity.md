# A corporate action is stored against its ticker and attributed through validity

Both providers answer for a ticker, never for an instrument. The series a provider
serves under a ticker is its history for the symbol. That history may hold the corporate
actions of another company that used the symbol earlier.

A corporate action is stored as fetched, against the ticker and its market, with the fetch
key that fetched it. Coverage is stored the same way. When holdings are read, a corporate
action belongs to the instrument that its ticker named on the ex-date. Validity decides
this, confirmed or provisional, exactly as it decides the instrument of a dated
transaction. A new identifier fact moves the attribution of a corporate action with no
write.

A transaction of a dated key and the corporate action that restates it land on one
instrument, because validity attributes both. A transaction of a fixed key belongs to its
instrument for all time. It is restated by the corporate actions that validity attributes
to that instrument.

Each transaction is restated from one datasource and one market. They are the first
datasource by precedence, and a market, whose coverage of the tickers that named the
instrument in that market spans the period from the transaction's as-at date to today.
Two datasources, or two markets, never contribute corporate actions to one transaction.
When no datasource and market span the period, the transaction is uncovered.

Every fetch asks for the whole period that the held transactions need. Each provider
returns the whole history of a ticker in one request, so a shorter period saves no
calls. The latest answer of a datasource for a ticker, by the moment the datasource gave
it, replaces its earlier answers. A provider that deletes or re-dates a corporate action is
therefore followed at the next fetch.

A fetch compares its corporate actions with those of every other datasource that covers
the same ticker and period. It records a `corp_act_disagreement` finding when a
corporate action of one lacks a corporate action of equal ratio in the other within 7
days.

## Considered options

- Store a corporate action against the instrument and unwind it when an identifier event
  shows the ticker named another instrument. The holding is wrong from the fetch until
  the identifier event arrives.
- Store a corporate action against the instrument and discard those outside the ticker's
  confirmed validity. This loses data that a later fact could attribute.
- Choose the datasource for each ticker and day by precedence. A split that two
  datasources date a day apart, at the boundary between their coverage, is applied
  twice. A split that two markets report for one instrument is also applied twice.
- Fetch only the period that coverage lacks, overlapping the last fetch by a lag. This
  needs a lag for each kind of fact, and it misses a provider's later correction of an
  older corporate action.
