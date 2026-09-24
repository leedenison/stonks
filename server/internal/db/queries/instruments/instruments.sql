-- name: CreateInstrument :one
INSERT INTO instruments (id, asset_class)
VALUES ($1, $2)
RETURNING *;

-- name: CreateListing :one
INSERT INTO listings (id, instrument_id, currency)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateIdentifier :one
INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id = $1
ORDER BY type, domain, value;

-- name: ListCurrencies :many
SELECT code FROM currencies ORDER BY code;

-- name: ListAssetClassTree :many
SELECT * FROM asset_class_tree ORDER BY class;

-- name: CreateResolutionKey :exec
INSERT INTO resolution_keys (run_id, user_id, stated_key_id, outcome, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: ListResolutionKeys :many
SELECT * FROM resolution_keys
WHERE run_id = $1 AND user_id = $2
ORDER BY stated_key_id;

-- name: GetInstrumentByIdentifier :one
SELECT sqlc.embed(identifiers), sqlc.embed(instruments)
FROM identifiers
JOIN instruments ON instruments.id = identifiers.instrument_id
WHERE identifiers.listing_id IS NULL
  AND identifiers.type = @type
  AND identifiers.domain = @domain::text
  AND identifiers.value = @value::text;

-- name: GetListing :one
SELECT * FROM listings
WHERE instrument_id = $1 AND currency = $2;

-- name: ListResolutionItems :many
SELECT sqlc.embed(resolution_keys), sqlc.embed(stated_keys)
FROM resolution_keys
JOIN stated_keys ON stated_keys.id = resolution_keys.stated_key_id
WHERE resolution_keys.run_id = $1
ORDER BY resolution_keys.stated_key_id;
