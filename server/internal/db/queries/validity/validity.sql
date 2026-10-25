-- name: CreateIdentifierAssertion :exec
INSERT INTO identifier_assertions (id, fetch_key_id, run_id, attested_by, type, domain, value,
    instrument_id, asserted_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListFetchKeyAssertions :many
SELECT * FROM identifier_assertions WHERE fetch_key_id = $1
ORDER BY type, domain, value;

-- name: RelinkIdentifierAssertions :exec
UPDATE identifier_assertions SET instrument_id = @survivor::uuid WHERE instrument_id = @loser::uuid;

-- name: ListIdentifierAssertions :many
-- The assertions of one identifier, each with the datasource of the fetch
-- that made it and that datasource's precedence, or the email of the
-- administrator who attested it.
SELECT sqlc.embed(identifier_assertions), fetches.datasource, datasources.precedence,
       users.email AS attested_by
FROM identifier_assertions
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN datasources ON datasources.name = fetches.datasource
LEFT JOIN users ON users.id = identifier_assertions.attested_by
WHERE identifier_assertions.type = @type
  AND identifier_assertions.domain = @domain
  AND identifier_assertions.value = @value
ORDER BY identifier_assertions.asserted_on, identifier_assertions.id;

-- name: ListIdentifierEvents :many
-- The events that name a ticker, or that renamed another ticker to it, each
-- with the fetch key that wrote it.
SELECT value, action, to_value, effective_on, fetch_key_id FROM identifier_events
WHERE domain = @domain AND (value = @value OR to_value = @value)
ORDER BY effective_on, id;

-- name: ListIdentifierCoverage :many
-- The coverage of a ticker, and of its domain, each with the datasource whose
-- fetch wrote it. value is NULL for a domain row.
SELECT identifier_event_coverage.domain, identifier_event_coverage.value,
       identifier_event_coverage.covered_from, identifier_event_coverage.covered_before,
       fetches.datasource
FROM identifier_event_coverage
JOIN fetch_keys ON fetch_keys.id = identifier_event_coverage.fetch_key_id
JOIN fetches ON fetches.id = fetch_keys.fetch_id
WHERE identifier_event_coverage.domain = @domain
  AND (identifier_event_coverage.value = @value OR identifier_event_coverage.value IS NULL)
ORDER BY fetches.datasource, identifier_event_coverage.covered_from, identifier_event_coverage.id;

-- name: ListDomainCoverage :many
SELECT covered_from, covered_before FROM identifier_event_coverage
WHERE domain = @domain AND value IS NULL
ORDER BY covered_from;

-- name: FirstDomainTransaction :one
-- The earliest order date of a transaction whose key states a MIC_TICKER on
-- the domain, or on a venue operated under it.
SELECT transactions.order_date
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
CROSS JOIN LATERAL jsonb_array_elements(stated_keys.identifiers) AS stated(id)
LEFT JOIN mics ON mics.mic = stated.id->>'domain'
WHERE stated.id->>'type' = 'mic_ticker'
  AND coalesce(mics.operating_mic, stated.id->>'domain') = @domain::text
ORDER BY transactions.order_date
LIMIT 1;

-- name: CreateIdentifierEvent :execrows
-- An event is written only from a served fetch key.
INSERT INTO identifier_events (id, fetch_key_id, domain, value, action, to_value, effective_on)
SELECT @id, fetch_keys.id, @domain, @value, @action, sqlc.narg(to_value), @effective_on
FROM fetch_keys WHERE fetch_keys.id = @fetch_key_id AND fetch_keys.outcome = 'served';

-- name: CreateIdentifierEventCoverage :execrows
-- Coverage is written only from a served fetch key.
INSERT INTO identifier_event_coverage (id, fetch_key_id, domain, value, covered_from, covered_before)
SELECT @id, fetch_keys.id, @domain, sqlc.narg(value), @covered_from, @covered_before
FROM fetch_keys WHERE fetch_keys.id = @fetch_key_id AND fetch_keys.outcome = 'served';

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

-- name: ListRunAssertions :many
-- The assertions a run wrote: those of the fetch runs it started, and those
-- it attested. Each comes with the datasource of its fetch, or the email of
-- the administrator who attested it. A NULL after starts at the first, and a
-- NULL lim reads every one.
SELECT sqlc.embed(identifier_assertions), fetches.datasource, users.email AS attested_by
FROM identifier_assertions
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN runs ON runs.id = fetches.id
LEFT JOIN users ON users.id = identifier_assertions.attested_by
WHERE (runs.parent_id = @run_id OR identifier_assertions.run_id = @run_id)
  AND (sqlc.narg(after)::uuid IS NULL OR identifier_assertions.id > sqlc.narg(after))
ORDER BY identifier_assertions.id
LIMIT sqlc.narg(lim)::int;

-- name: ListTickerDomains :many
-- The operating MICs on which keys with a transaction state tickers, each
-- with its count of distinct tickers. A venue counts under its operating MIC,
-- and a ticker stated without a venue counts nowhere.
SELECT coalesce(mics.operating_mic, stated.id->>'domain')::text AS domain,
       count(DISTINCT stated.id->>'value')::int AS tickers
FROM stated_keys
CROSS JOIN LATERAL jsonb_array_elements(stated_keys.identifiers) AS stated(id)
LEFT JOIN mics ON mics.mic = stated.id->>'domain'
WHERE stated.id->>'type' = 'mic_ticker'
  AND coalesce(stated.id->>'domain', '') <> ''
  AND EXISTS (SELECT 1 FROM transactions WHERE transactions.stated_key_id = stated_keys.id)
GROUP BY coalesce(mics.operating_mic, stated.id->>'domain')
ORDER BY domain;

-- name: ListDomainTickers :many
-- The distinct tickers that keys with a transaction state on the domain, or
-- on a venue operated under it.
SELECT DISTINCT (stated.id->>'value')::text AS value
FROM stated_keys
CROSS JOIN LATERAL jsonb_array_elements(stated_keys.identifiers) AS stated(id)
LEFT JOIN mics ON mics.mic = stated.id->>'domain'
WHERE stated.id->>'type' = 'mic_ticker'
  AND coalesce(stated.id->>'domain', '') <> ''
  AND coalesce(mics.operating_mic, stated.id->>'domain') = @domain::text
  AND EXISTS (SELECT 1 FROM transactions WHERE transactions.stated_key_id = stated_keys.id)
ORDER BY value;

-- name: ListTickerTransactions :many
-- The transactions of every key that states the ticker on the domain, or on
-- a venue operated under it.
SELECT transactions.stated_key_id, transactions.order_date
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(stated_keys.identifiers) AS stated(id)
    LEFT JOIN mics ON mics.mic = stated.id->>'domain'
    WHERE stated.id->>'type' = 'mic_ticker'
      AND coalesce(mics.operating_mic, stated.id->>'domain') = @domain::text
      AND stated.id->>'value' = @value::text)
ORDER BY transactions.order_date, transactions.stated_key_id, transactions.id;
