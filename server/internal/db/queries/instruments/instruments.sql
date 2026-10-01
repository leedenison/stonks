-- name: CreateInstrument :one
INSERT INTO instruments (id, asset_class, fetch_key_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateListing :one
INSERT INTO listings (id, instrument_id, currency, fetch_key_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListListings :many
SELECT * FROM listings
WHERE instrument_id = $1
ORDER BY currency;

-- name: CreateIdentifier :one
INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value, fetch_key_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id = $1
ORDER BY type, domain, value;

-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY code;

-- name: ListMICs :many
SELECT mic, operating_mic FROM mics ORDER BY mic;

-- name: ListAssetClassTree :many
SELECT * FROM asset_class_tree ORDER BY class;

-- name: CreateResolutionKey :exec
INSERT INTO resolution_keys (run_id, user_id, stated_key_id, outcome, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: ListResolutionKeys :many
SELECT * FROM resolution_keys
WHERE run_id = $1 AND user_id = $2
ORDER BY stated_key_id;

-- name: FindIdentifier :one
SELECT sqlc.embed(identifiers), sqlc.embed(instruments)
FROM identifiers
JOIN instruments ON instruments.id = identifiers.instrument_id
WHERE identifiers.type = @type
  AND identifiers.domain = @domain::text
  AND identifiers.value = @value::text;

-- name: ListInstrumentsByIdentifiers :many
-- The identifiers held among those given, at either grain, with the
-- instruments they name.
SELECT sqlc.embed(identifiers), sqlc.embed(instruments)
FROM (SELECT unnest(@types::text[]) AS type, unnest(@domains::text[]) AS domain,
             unnest(@values::text[]) AS value) AS k
JOIN identifiers ON identifiers.type::text = k.type
  AND identifiers.domain = k.domain
  AND identifiers.value = k.value
JOIN instruments ON instruments.id = identifiers.instrument_id
ORDER BY instruments.id, identifiers.type, identifiers.domain, identifiers.value;

-- name: LockIdentifiers :exec
-- Serialises creation on the identifiers a stated key admits, for the
-- transaction. The caller passes them sorted, so two transactions take the
-- locks in one order. The seed keeps the keys apart from LockUserKeys.
SELECT pg_advisory_xact_lock(hashtextextended(k, 1)) FROM unnest(@keys::text[]) AS k;

-- name: GetListing :one
SELECT * FROM listings
WHERE instrument_id = $1 AND currency = $2;

-- name: ListResolutionItems :many
SELECT sqlc.embed(resolution_keys), sqlc.embed(stated_keys)
FROM resolution_keys
JOIN stated_keys ON stated_keys.id = resolution_keys.stated_key_id
WHERE resolution_keys.run_id = $1
ORDER BY resolution_keys.stated_key_id;
