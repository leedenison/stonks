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

## Telemetry

The development stack collects traces and metrics from the service and serves them from
Grafana at http://localhost:3000, signing in with `admin` and `stonks`. Prometheus is at
http://localhost:9090. Both listen on the loopback interface only.

Datasources and dashboards are provisioned from `docker/grafana`. A dashboard edited in
the browser cannot be saved: to change a panel, edit it there, export the JSON and commit
it over the file it came from.

No other stack configures a collector endpoint, so the service exports nothing when it is
run for tests or end to end.

