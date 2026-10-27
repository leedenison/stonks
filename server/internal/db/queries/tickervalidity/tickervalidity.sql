-- name: CreateIdentifierAssertion :exec
INSERT INTO identifier_assertions (id, fetch_key_id, run_id, attested_by, type, domain, value,
    instrument_id, asserted_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CreateIdentifierAttestation :exec
-- A later attestation of a day replaces the earlier one.
INSERT INTO identifier_assertions (id, run_id, attested_by, type, domain, value, instrument_id, asserted_on)
VALUES (@id, @run_id::uuid, @attested_by::uuid, @type, @domain, @value, @instrument_id, @asserted_on)
ON CONFLICT (type, domain, value, asserted_on) WHERE run_id IS NOT NULL
DO UPDATE SET run_id = EXCLUDED.run_id, attested_by = EXCLUDED.attested_by,
    instrument_id = EXCLUDED.instrument_id;

-- name: ListFetchKeyAssertions :many
-- Only tests read a fetch key's assertions.
SELECT * FROM identifier_assertions WHERE fetch_key_id = $1
ORDER BY type, domain, value;

-- name: RelinkIdentifierAssertions :exec
UPDATE identifier_assertions SET instrument_id = @survivor::uuid WHERE instrument_id = @loser::uuid;

-- name: ListIdentifierAssertions :many
-- The assertions of the tickers whose market is among markets and whose value
-- is among values. The caller keeps the pairs it asked for. Each assertion
-- comes with the datasource of the fetch that made it and that datasource's
-- precedence, or with the email of the administrator who attested it.
SELECT sqlc.embed(identifier_assertions), fetches.datasource, datasources.precedence,
       users.email AS attested_by
FROM identifier_assertions
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN datasources ON datasources.name = fetches.datasource
LEFT JOIN users ON users.id = identifier_assertions.attested_by
WHERE identifier_assertions.type = 'mic_ticker'
  AND identifier_assertions.domain = ANY(@markets::text[])
  AND identifier_assertions.value = ANY(@values::text[])
ORDER BY identifier_assertions.asserted_on, identifier_assertions.id;

-- name: ListIdentifierEvents :many
-- The events in a market among markets that name a ticker whose value is
-- among values, or that renamed another ticker to it. The caller keeps the
-- pairs it asked for. Each event comes with the fetch key that wrote it.
SELECT market, value, action, to_value, effective_on, fetch_key_id FROM identifier_events
WHERE market = ANY(@markets::text[])
  AND (value = ANY(@values::text[]) OR to_value = ANY(@values::text[]))
ORDER BY effective_on, id;

-- name: ListIdentifierCoverage :many
-- The coverage in a market among markets of a ticker whose value is among
-- values, and of the market itself. The caller keeps the pairs it asked for.
-- Each row comes with the datasource whose fetch wrote it. value is NULL for
-- a market row.
SELECT identifier_event_coverage.market, identifier_event_coverage.value,
       identifier_event_coverage.covered_from, identifier_event_coverage.covered_before,
       identifier_event_coverage.fetch_key_id, fetches.datasource
FROM identifier_event_coverage
JOIN fetch_keys ON fetch_keys.id = identifier_event_coverage.fetch_key_id
JOIN fetches ON fetches.id = fetch_keys.fetch_id
WHERE identifier_event_coverage.market = ANY(@markets::text[])
  AND (identifier_event_coverage.value = ANY(@values::text[]) OR identifier_event_coverage.value IS NULL)
ORDER BY fetches.datasource, identifier_event_coverage.covered_from, identifier_event_coverage.id;

-- name: CreateIdentifierEvent :exec
INSERT INTO identifier_events (id, fetch_key_id, market, value, action, to_value, effective_on)
VALUES (@id, @fetch_key_id, @market, @value, @action, sqlc.narg(to_value), @effective_on);

-- name: CreateIdentifierEventCoverage :exec
INSERT INTO identifier_event_coverage (id, fetch_key_id, market, value, covered_from, covered_before)
VALUES (@id, @fetch_key_id, @market, sqlc.narg(value), @covered_from, @covered_before);

-- name: ListHeldTickers :many
-- The held tickers: each MIC_TICKER through which a key is dated, where a
-- transaction names the key. Each comes with the asset class and currency of
-- the oldest such key.
SELECT DISTINCT ON (via_domain, via_value)
       via_type::identifier_type AS type, via_domain::text AS domain, via_value::text AS value,
       asset_class, currency
FROM stated_keys
WHERE via_type IS NOT NULL
  AND EXISTS (SELECT 1 FROM transactions WHERE transactions.stated_key_id = stated_keys.id)
ORDER BY via_domain, via_value, created_at, id;

-- name: ListHeldInstruments :many
-- Each instrument that a fixed key names, where a transaction names the key,
-- with each user of such a transaction and the earliest as-at date of that
-- user's transactions.
SELECT stated_keys.instrument_id::uuid AS instrument_id, transactions.user_id,
       MIN(transactions.as_at)::date AS first_as_at
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE stated_keys.instrument_id IS NOT NULL
GROUP BY stated_keys.instrument_id, transactions.user_id
ORDER BY stated_keys.instrument_id, transactions.user_id;

-- name: ListHeldTickerDates :many
-- Each as-at date of a transaction of a dated key, with the ticker through
-- which the key is dated and the user, once each.
SELECT DISTINCT stated_keys.via_domain::text AS domain, stated_keys.via_value::text AS value,
       transactions.user_id, transactions.as_at
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE stated_keys.via_type IS NOT NULL
ORDER BY domain, value, transactions.user_id, transactions.as_at;

-- name: ListRunAssertions :many
-- The assertions a run wrote: those of the fetch runs it started, and those
-- it attested. Each comes with the datasource of its fetch, or the email of
-- the administrator who attested it. A NULL after starts at the first, and a
-- NULL lim reads every one. The two sources are gathered apart so that each
-- reads through its own index.
SELECT sqlc.embed(identifier_assertions), fetches.datasource, users.email AS attested_by
FROM identifier_assertions
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

-- name: ListInstrumentTickers :many
-- The tickers of the instrument at their markets: those on its listings, and
-- those its assertions name. Each comes with whether a listing carries it,
-- and the operating MICs of the listings that do.
SELECT named.market::text AS market, named.value::text AS value, bool_or(named.current)::bool AS current,
       COALESCE(array_agg(DISTINCT named.venue ORDER BY named.venue) FILTER (WHERE named.venue IS NOT NULL), '{}')::text[] AS venues
FROM (
    SELECT mic_markets.market, identifiers.value, true AS current, mic_markets.operating_mic AS venue
    FROM identifiers
    JOIN mic_markets ON mic_markets.mic = identifiers.domain
    WHERE identifiers.instrument_id = @instrument AND identifiers.type = 'mic_ticker'
    UNION ALL
    SELECT domain, value, false, NULL FROM identifier_assertions
    WHERE identifier_assertions.instrument_id = @instrument AND type = 'mic_ticker'
) AS named
GROUP BY named.market, named.value
ORDER BY named.market, named.value;

-- name: CountTickerTransactions :one
-- The count of transactions of every key that states one of the probes; see
-- ListUnassociatedKeysStating.
SELECT count(*)::int
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE stated_keys.identifiers @> ANY(@probes::jsonb[]);

-- name: ListTickerTransactions :many
-- The transactions of every key that states one of the probes; see
-- ListUnassociatedKeysStating.
SELECT transactions.stated_key_id, transactions.as_at
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE stated_keys.identifiers @> ANY(@probes::jsonb[])
ORDER BY transactions.as_at, transactions.stated_key_id, transactions.id;

-- name: GetIdentifierAssertion :one
SELECT * FROM identifier_assertions WHERE id = $1;

-- name: DeleteAttestedAssertion :execrows
DELETE FROM identifier_assertions WHERE id = $1 AND run_id IS NOT NULL;
