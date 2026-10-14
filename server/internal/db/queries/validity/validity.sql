-- name: CreateIdentifierAssertion :exec
INSERT INTO identifier_assertions (id, fetch_key_id, run_id, attested_by, type, domain, value,
    instrument_id, asserted_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListFetchKeyAssertions :many
SELECT * FROM identifier_assertions WHERE fetch_key_id = $1
ORDER BY type, domain, value;

-- name: RelinkIdentifierAssertions :exec
UPDATE identifier_assertions SET instrument_id = @survivor::uuid WHERE instrument_id = @loser::uuid;

-- name: ListValueAssertions :many
-- The assertions of one identifier, each with the datasource whose fetch made
-- it and that datasource's precedence. Both are NULL for an attestation.
SELECT identifier_assertions.asserted_on, identifier_assertions.instrument_id,
       fetches.datasource, datasources.precedence
FROM identifier_assertions
LEFT JOIN fetch_keys ON fetch_keys.id = identifier_assertions.fetch_key_id
LEFT JOIN fetches ON fetches.id = fetch_keys.fetch_id
LEFT JOIN datasources ON datasources.name = fetches.datasource
WHERE identifier_assertions.type = @type
  AND identifier_assertions.domain = @domain
  AND identifier_assertions.value = @value
ORDER BY identifier_assertions.asserted_on, identifier_assertions.id;

-- name: ListValueEventDays :many
-- The days of the events that name a value, or that renamed another value to
-- it.
SELECT DISTINCT effective_on FROM identifier_events
WHERE domain = @domain AND (value = @value OR to_value = @value)
ORDER BY effective_on;

-- name: ListValueCoverage :many
-- The coverage of a value, and of its domain.
SELECT covered_from, covered_before FROM identifier_event_coverage
WHERE domain = @domain AND (value = @value OR value IS NULL)
ORDER BY covered_from;

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
