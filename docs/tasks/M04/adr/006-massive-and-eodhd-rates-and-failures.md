# Massive and EODHD rates and failures

## Massive plans

Massive's rate depends on the account's plan, and the credential does not reveal the
plan. The datasources table gains a `config` jsonb column, which holds settings that are
specific to one integration. Massive's config names the selected plan and gives each
plan's rate, so the rate is configuration. The seed migration writes Massive's config
with the published rates: Basic at five requests a minute, and Starter, Developer and
Advanced without a limit. The selected plan is Basic. An administrator edits the config in the admin area alongside the credential.

Massive limits each asset class separately. Only the stocks rate applies, since the
integration asks for nothing else.

## EODHD quota

EODHD allows a thousand requests a minute, and a daily quota of calls that resets at
midnight GMT. A 402 means the quota is spent. It is a temporary failure of the whole
datasource, and the integration states the time until midnight GMT as its Retry-After.

The fetch framework pauses the datasource until then; see
[market.go](../../../../server/internal/market/market.go).

## Failures

For both providers:

- a 401 is permanent and blocks the datasource;
- a 403 is permanent. On Massive it blocks the datasource. On EODHD it means the key is
  not entitled to the symbol, and it blocks only the identifier sent;
- a 429 is temporary, and when the provider states a Retry-After the next call waits for it;
- a 5xx or a network error is temporary;
- an empty answer is a served fetch with no candidates.

EODHD charges a 404 like any other call. Massive's 429 omits the Retry-After, so its
retry follows the framework's own schedule.
