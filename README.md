# Stonks

Stonks is multi-user portfolio tracking software. It consists of backend services hosted
in docker containers which serve a web based front end.

Stonks tracks the holdings of the instruments in a user's portfolios, and calculates the
valuation of those portfolios from the prices of the instruments held.

## Prerequisites

* Docker (with Compose v2)
* GNU make

## Getting started

```sh
cp .env.example .env
# then set GOOGLE_OAUTH_CLIENT_ID in .env
make run
```

The application is served at http://localhost:8080.

Run `make help` for the full list of targets.
