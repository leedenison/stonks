-- name: CreateIdentifierAssertion :exec
INSERT INTO identifier_assertions (id, fetch_key_id, run_id, attested_by, ticker_id, market,
    instr_id, asserted_on, edge, venue, currency)
VALUES (@id, @fetch_key_id, @run_id, @attested_by, @ticker_id, @market, @instr_id, @asserted_on,
    sqlc.narg(edge), sqlc.narg(venue), sqlc.narg(currency));

-- name: CreateIdentifierAttestation :exec
-- A later identifier attestation of a day replaces the earlier one.
INSERT INTO identifier_assertions (id, run_id, attested_by, ticker_id, market, instr_id, asserted_on)
VALUES (@id, @run_id::uuid, @attested_by::uuid, @ticker_id, @market, @instr_id, @asserted_on)
ON CONFLICT (ticker_id, asserted_on) WHERE run_id IS NOT NULL
DO UPDATE SET run_id = EXCLUDED.run_id, attested_by = EXCLUDED.attested_by,
    instr_id = EXCLUDED.instr_id;

-- name: ListFetchKeyAssertions :many
-- Only tests read a fetch key's assertions.
SELECT sqlc.embed(identifier_assertions), tickers.value
FROM identifier_assertions
JOIN tickers ON tickers.id = identifier_assertions.ticker_id
WHERE identifier_assertions.fetch_key_id = $1
ORDER BY tickers.market, tickers.value;

-- name: RelinkIdentifierAssertions :exec
UPDATE identifier_assertions SET instr_id = @survivor::uuid WHERE instr_id = @loser::uuid;

-- name: ListIdentifierAssertions :many
-- The assertions of the tickers whose market and value are a pair the arrays
-- hold at one index. Each assertion comes with its ticker's value, with the
-- datasource of the fetch that made it and that datasource's precedence, or
-- with the email of the administrator who attested it.
SELECT sqlc.embed(identifier_assertions), tickers.value, fetches.datasource, datasources.precedence,
       users.email AS attested_by
FROM (SELECT unnest(@markets::text[]) AS market, unnest(@values::text[]) AS value) AS asked
JOIN tickers ON tickers.market = asked.market AND tickers.value = asked.value
JOIN identifier_assertions ON identifier_assertions.ticker_id = tickers.id
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN datasources ON datasources.name = fetches.datasource
LEFT JOIN users ON users.id = identifier_assertions.attested_by
ORDER BY identifier_assertions.asserted_on, identifier_assertions.id;

-- name: ListIdentifierPeriodAssertions :many
-- The period assertions of the tickers whose market and value are a pair the
-- arrays hold at one index, and of every market among markets. Each row comes
-- with the datasource whose fetch wrote it and that datasource's precedence.
-- value is NULL for a market row.
WITH asked AS (
    SELECT tickers.id
    FROM (SELECT unnest(@markets::text[]) AS market, unnest(@values::text[]) AS value) AS k
    JOIN tickers ON tickers.market = k.market AND tickers.value = k.value
)
SELECT identifier_period_assertions.market, tickers.value,
       identifier_period_assertions.asserted_from, identifier_period_assertions.asserted_before,
       identifier_period_assertions.fetch_key_id, fetches.datasource, datasources.precedence
FROM identifier_period_assertions
LEFT JOIN tickers ON tickers.id = identifier_period_assertions.ticker_id
JOIN fetch_keys ON fetch_keys.id = identifier_period_assertions.fetch_key_id
JOIN fetches ON fetches.id = fetch_keys.fetch_id
JOIN datasources ON datasources.name = fetches.datasource
WHERE identifier_period_assertions.ticker_id IN (SELECT id FROM asked)
   OR (identifier_period_assertions.ticker_id IS NULL
       AND identifier_period_assertions.market = ANY(@markets::text[]))
ORDER BY fetches.datasource, identifier_period_assertions.asserted_from, identifier_period_assertions.id;

-- name: CreateIdentifierPeriodAssertion :exec
INSERT INTO identifier_period_assertions (id, fetch_key_id, market, ticker_id, asserted_from, asserted_before)
VALUES (@id, @fetch_key_id, @market, sqlc.narg(ticker_id), @asserted_from, @asserted_before);

-- name: ListHeldTickers :many
-- The held tickers: each ticker and venue through which a key is dated,
-- where a transaction names the key. Each comes with the asset class and
-- currency of the oldest such key.
SELECT DISTINCT ON (stated_keys.via_venue, stated_keys.via_ticker_id)
       stated_keys.via_ticker_id::uuid AS ticker_id, stated_keys.via_venue::text AS venue,
       tickers.market, tickers.value, stated_keys.asset_class, stated_keys.currency
FROM stated_keys
JOIN tickers ON tickers.id = stated_keys.via_ticker_id
WHERE EXISTS (SELECT 1 FROM txns WHERE txns.stated_key_id = stated_keys.id)
ORDER BY stated_keys.via_venue, stated_keys.via_ticker_id, stated_keys.created_at, stated_keys.id;

-- name: ListHeldInstrs :many
-- Each instrument that a fixed key names, where a transaction names the key,
-- with each user of such a transaction and the earliest as-at date of that
-- user's transactions.
SELECT stated_keys.instr_id::uuid AS instr_id, txns.user_id,
       MIN(txns.as_at)::date AS first_as_at
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
WHERE stated_keys.instr_id IS NOT NULL
GROUP BY stated_keys.instr_id, txns.user_id
ORDER BY stated_keys.instr_id, txns.user_id;

-- name: ListHeldTickerDates :many
-- Each as-at date of a transaction of a dated key, with the ticker through
-- which the key is dated and the user, once each.
SELECT DISTINCT stated_keys.via_ticker_id::uuid AS ticker_id, tickers.market, tickers.value,
       txns.user_id, txns.as_at
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
JOIN tickers ON tickers.id = stated_keys.via_ticker_id
ORDER BY tickers.market, tickers.value, txns.user_id, txns.as_at;

-- name: ListRunAssertions :many
-- The assertions a run wrote: those of the fetch runs it started, and those
-- it attested. Each comes with its ticker's value, and with the datasource of
-- its fetch or the email of the administrator who attested it. A NULL after
-- starts at the first, and a NULL lim reads every one. The two sources are
-- gathered apart so that each reads through its own index.
SELECT sqlc.embed(identifier_assertions), tickers.value, fetches.datasource, users.email AS attested_by
FROM identifier_assertions
JOIN tickers ON tickers.id = identifier_assertions.ticker_id
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN users ON users.id = identifier_assertions.attested_by
WHERE identifier_assertions.id IN (
        SELECT attested.id FROM identifier_assertions AS attested WHERE attested.run_id = @run_id
        UNION ALL
        SELECT fetched.id FROM identifier_assertions AS fetched
        JOIN fetch_keys AS keys ON keys.id = fetched.fetch_key_id
        JOIN runs ON runs.id = keys.fetch_id
        WHERE runs.parent_id = @run_id)
  AND (sqlc.narg(after)::uuid IS NULL OR identifier_assertions.id > sqlc.narg(after))
ORDER BY identifier_assertions.id
LIMIT sqlc.narg(lim)::int;

-- name: ListInstrTickers :many
-- The tickers of the instrument at their markets: those on its listings, and
-- those its assertions name. Each comes with whether a listing carries it,
-- and the operating MICs of the listings that do.
SELECT named.market::text AS market, named.value::text AS value, bool_or(named.current)::bool AS current,
       COALESCE(array_agg(DISTINCT named.venue ORDER BY named.venue) FILTER (WHERE named.venue IS NOT NULL), '{}')::text[] AS venues
FROM (
    SELECT mic_markets.market, identifiers.value, true AS current, mic_markets.operating_mic AS venue
    FROM identifiers
    JOIN mic_markets ON mic_markets.mic = identifiers.domain
    WHERE identifiers.instr_id = @instr AND identifiers.type = 'mic_ticker'
    UNION ALL
    SELECT tickers.market, tickers.value, false, NULL
    FROM identifier_assertions
    JOIN tickers ON tickers.id = identifier_assertions.ticker_id
    WHERE identifier_assertions.instr_id = @instr
) AS named
GROUP BY named.market, named.value
ORDER BY named.market, named.value;

-- name: CountTickerTxns :one
-- The count of transactions of every key that states one of the probes; see
-- ListUnassociatedKeysStating.
SELECT count(*)::int
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
WHERE stated_keys.identifiers @> ANY(@probes::jsonb[]);

-- name: ListTickerTxns :many
-- The transactions of every key that states one of the probes; see
-- ListUnassociatedKeysStating.
SELECT txns.stated_key_id, txns.as_at
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
WHERE stated_keys.identifiers @> ANY(@probes::jsonb[])
ORDER BY txns.as_at, txns.stated_key_id, txns.id;

-- name: GetIdentifierAssertion :one
SELECT sqlc.embed(identifier_assertions), tickers.value
FROM identifier_assertions
JOIN tickers ON tickers.id = identifier_assertions.ticker_id
WHERE identifier_assertions.id = $1;

-- name: DeleteAttestedAssertion :execrows
DELETE FROM identifier_assertions WHERE id = $1 AND run_id IS NOT NULL;

-- name: ListResolvePairs :many
-- Each instrument and market whose names the tickers' facts or rows of
-- resolved_tickers touch.
SELECT DISTINCT instr_id::uuid AS instr_id, market FROM (
    SELECT instr_id, market FROM identifier_assertions
    WHERE ticker_id = ANY(@ticker_ids::uuid[]) AND instr_id IS NOT NULL
    UNION
    SELECT instr_id, market FROM resolved_tickers
    WHERE ticker_id = ANY(@ticker_ids::uuid[])
) AS pairs
ORDER BY instr_id, market;

-- name: ListPairTickers :many
-- For each instrument and market that the arrays hold at one index, the
-- tickers of the market with a positive assertion that names the instrument.
SELECT DISTINCT asked.instr_id::uuid AS instr_id, sqlc.embed(tickers)
FROM (SELECT unnest(@instr_ids::uuid[]) AS instr_id, unnest(@markets::text[]) AS market) AS asked
JOIN identifier_assertions ON identifier_assertions.instr_id = asked.instr_id
    AND identifier_assertions.market = asked.market
JOIN tickers ON tickers.id = identifier_assertions.ticker_id
ORDER BY asked.instr_id, tickers.market, tickers.value;

-- name: DeleteResolvedTickers :exec
-- Deletes the rows of each instrument and market that the arrays hold at one
-- index.
DELETE FROM resolved_tickers
USING (SELECT unnest(@instr_ids::uuid[]) AS instr_id, unnest(@markets::text[]) AS market) AS asked
WHERE resolved_tickers.instr_id = asked.instr_id AND resolved_tickers.market = asked.market;

-- name: CreateResolvedTickers :exec
-- The bounds are ISO dates. An empty venue, currency or bound is stored as
-- NULL.
INSERT INTO resolved_tickers (ticker_id, market, instr_id, venue, valid_from, valid_before, currency)
SELECT r.ticker_id, r.market, r.instr_id, NULLIF(r.venue, ''), NULLIF(r.valid_from, '')::date,
       NULLIF(r.valid_before, '')::date, NULLIF(r.currency, '')
FROM (SELECT unnest(@ticker_ids::uuid[]) AS ticker_id, unnest(@markets::text[]) AS market,
             unnest(@instr_ids::uuid[]) AS instr_id, unnest(@venues::text[]) AS venue,
             unnest(@valid_froms::text[]) AS valid_from, unnest(@valid_befores::text[]) AS valid_before,
             unnest(@currencies::text[]) AS currency) AS r;

-- name: ListResolvedTickers :many
-- Only tests read the rows of an instrument.
SELECT resolved_tickers.*, tickers.value
FROM resolved_tickers
JOIN tickers ON tickers.id = resolved_tickers.ticker_id
WHERE resolved_tickers.instr_id = ANY(@instr_ids::uuid[])
ORDER BY resolved_tickers.instr_id, resolved_tickers.market, resolved_tickers.venue NULLS FIRST;
