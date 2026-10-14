-- name: CreateIdentifierAssertion :exec
INSERT INTO identifier_assertions (id, fetch_key_id, run_id, attested_by, type, domain, value,
    instrument_id, asserted_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListFetchKeyAssertions :many
SELECT * FROM identifier_assertions WHERE fetch_key_id = $1
ORDER BY type, domain, value;

-- name: RelinkIdentifierAssertions :exec
UPDATE identifier_assertions SET instrument_id = @survivor::uuid WHERE instrument_id = @loser::uuid;
