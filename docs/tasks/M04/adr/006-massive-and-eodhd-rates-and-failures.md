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

The fetch framework holds a datasource for the Retry-After a provider states, but caps
the hold at a few seconds and applies it only to a failure of one identifier. A spent
quota needs a hold that lasts hours. The framework therefore holds the datasource for the
whole Retry-After of a temporary datasource failure. While the hold lasts, it fails each
key it would have sent as temporary without calling the provider, instead of waiting.
A held datasource records no coverage, so its keys are asked again once the hold ends.
The hold lives in the process, so a restart lifts it, and the first call after the
restart meets the 402 again.

## Failures

For both providers:

- a 401 or a 403 is permanent and blocks the datasource;
- a 429 is temporary, held for the Retry-After where the provider states one;
- a 5xx or a network error is temporary;
- an empty answer is a served fetch with no candidates.

EODHD charges a 404 like any other call. Massive's 429 omits the Retry-After, so its
retry follows the framework's own schedule.
