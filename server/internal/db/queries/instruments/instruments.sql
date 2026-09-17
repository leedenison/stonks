-- name: CreateInstrument :one
INSERT INTO instruments (id, asset_class, owner_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateListing :one
INSERT INTO listings (id, instrument_id, currency, owner_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateIdentifier :one
INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value, owner_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetListingByIdentifier :one
SELECT sqlc.embed(listings), instruments.asset_class
FROM identifiers
JOIN listings ON listings.id = identifiers.listing_id
JOIN instruments ON instruments.id = listings.instrument_id
WHERE identifiers.owner_id IS NOT DISTINCT FROM sqlc.narg(owner_id)::uuid
  AND identifiers.type = @type
  AND identifiers.domain IS NOT DISTINCT FROM sqlc.narg(domain)::text
  AND identifiers.value = @value::text;

-- name: ListIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id = $1
  AND (owner_id IS NULL OR owner_id = @user_id::uuid)
ORDER BY type, domain, value;

-- name: ListCurrencies :many
SELECT code FROM currencies ORDER BY code;

-- name: ListAssetClassTree :many
SELECT * FROM asset_class_tree ORDER BY class;

-- name: CreateResolutionKey :exec
INSERT INTO resolution_keys (run_id, user_id, stated_key_id, outcome, instrument_id, listing_id, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListResolutionKeys :many
SELECT * FROM resolution_keys
WHERE run_id = $1 AND user_id = $2
ORDER BY stated_key_id;

-- name: GetInstrumentByIdentifier :one
SELECT instruments.*
FROM identifiers
JOIN instruments ON instruments.id = identifiers.instrument_id
WHERE identifiers.listing_id IS NULL
  AND identifiers.owner_id IS NOT DISTINCT FROM sqlc.narg(owner_id)::uuid
  AND identifiers.type = @type
  AND identifiers.domain IS NOT DISTINCT FROM sqlc.narg(domain)::text
  AND identifiers.value = @value::text;

-- name: GetListing :one
SELECT * FROM listings
WHERE instrument_id = $1 AND currency = $2;
