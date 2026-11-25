# Prices are a plain table, and the read carries a close

The stack installs TimescaleDB for time series, and prices are the first one. The prices
table is a plain table all the same. It holds a few million rows at most, and every read
is by listing, so a hypertable would add a partition scheme and gain little.

The read fills a day without a session in the server. See
[005](005-a-fetch-covers-the-days-held.md).

## Considered options

`time_bucket_gapfill` with `locf` fills such days in the query. It carries a value
across a break in coverage, where the read must stop. The server would still test the
coverage of each carried day. The server also restates every point through the
corporate actions calendar, so it walks the series either way.
