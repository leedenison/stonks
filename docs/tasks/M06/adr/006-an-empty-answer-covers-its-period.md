# An empty answer covers its period

An answer covers its period whether or not it holds a corporate action. Most tickers never
split, so most answers are empty.

EODHD answers a symbol it does not know with a distinct refusal. That answer carries no
coverage, as for any key a datasource does not serve. See
[datasources.md](../../../deferred/datasources.md).

Massive returns the same empty answer for a ticker that has never split, a ticker it does not
know and a ticker it has dropped. It has dropped mutual funds from its splits endpoint. An
open-end fund lacks an exchange listing, so the fetch never asks Massive about one. Massive's
answer for a listed ticker it has dropped matches its answer for a ticker that has never
split. Where Massive has precedence, holdings of that ticker are restated without the splits
Massive lacks. If another datasource reports such a split, the fetch raises a
`corp_act_disagreement` finding for the administrator.

## Considered options

- Treat an empty answer as uncovered. Every holding of a ticker that has never split would
  then show its raw quantity, marked as unadjusted.
