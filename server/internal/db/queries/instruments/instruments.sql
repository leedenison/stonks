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
