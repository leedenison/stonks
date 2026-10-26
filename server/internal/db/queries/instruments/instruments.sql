-- name: CreateInstrument :one
INSERT INTO instruments (id, asset_class, fetch_key_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateListing :one
INSERT INTO listings (id, instrument_id, currency, fetch_key_id, primary_mic)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SetListingPrimary :exec
-- Sets the primary venue of a listing that has none.
UPDATE listings SET primary_mic = @primary_mic WHERE id = @id AND primary_mic IS NULL;

-- name: ListListingsOf :many
SELECT * FROM listings
WHERE instrument_id = ANY(@ids::uuid[])
ORDER BY instrument_id, currency;

-- name: ListIdentifiersOf :many
-- By the rank of the type as a name, then by domain and value, which is the
-- order the API states.
SELECT identifiers.* FROM identifiers
JOIN identifier_type_traits ON identifier_type_traits.type = identifiers.type
WHERE identifiers.instrument_id = ANY(@ids::uuid[])
ORDER BY identifiers.instrument_id, identifier_type_traits.name_rank, identifiers.domain, identifiers.value;

-- name: GetInstrument :one
SELECT * FROM instruments WHERE id = $1;

-- name: ListInstrumentsByID :many
SELECT * FROM instruments WHERE id = ANY(@ids::uuid[]) ORDER BY id;

-- name: ListInstrumentNames :many
SELECT * FROM instrument_names
WHERE instrument_id = ANY(@ids::uuid[])
ORDER BY instrument_id;

-- name: ListViaOf :many
-- The ids of the via identifiers recorded on the stated keys of the
-- instruments.
SELECT DISTINCT stated_keys.via_id::uuid AS id FROM stated_keys
WHERE stated_keys.instrument_id = ANY(@ids::uuid[]) AND stated_keys.via_id IS NOT NULL
ORDER BY id;

-- name: ListListings :many
SELECT * FROM listings
WHERE instrument_id = $1
ORDER BY currency;

-- name: CreateIdentifier :one
INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value, fetch_key_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: PlaceTicker :one
-- Writes a ticker onto a listing of its present holder. Where another
-- instrument carries the ticker, the row moves to this one, with the fetch
-- key that placed it.
INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value, fetch_key_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (type, domain, value) DO UPDATE
SET instrument_id = EXCLUDED.instrument_id, listing_id = EXCLUDED.listing_id,
    fetch_key_id = EXCLUDED.fetch_key_id
RETURNING *;

-- name: ListIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id = $1
ORDER BY type, domain, value;

-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY code;

-- name: ListMICs :many
SELECT mic, operating_mic, market FROM mic_markets ORDER BY mic;

-- name: ListAssetClassTree :many
SELECT * FROM asset_class_tree ORDER BY class;

-- name: ListIdentifierTypeTraits :many
SELECT * FROM identifier_type_traits ORDER BY type;

-- name: CreateResolutionKey :one
-- A match passes no reasons, which are stored as an empty array.
INSERT INTO resolution_keys (run_id, user_id, stated_key_id, outcome, reasons, offered)
VALUES (@run_id, @user_id, @stated_key_id, @outcome, COALESCE(@reasons::text[], '{}'), @offered)
RETURNING *;

-- name: ListLatestResolutions :many
-- Run ids order by creation, so the latest resolution of a key is its row
-- with the greatest run id. A key no resolution has reached has no row.
SELECT DISTINCT ON (stated_key_id) *
FROM resolution_keys
WHERE stated_key_id = ANY(@ids::uuid[]) AND user_id = @user_id::uuid
ORDER BY stated_key_id, run_id DESC;

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
-- The cast sits on the parameter, so the unique index on identifiers serves the
-- join.
SELECT sqlc.embed(identifiers), sqlc.embed(instruments)
FROM (SELECT unnest(@types::text[]) AS type, unnest(@domains::text[]) AS domain,
             unnest(@values::text[]) AS value) AS k
JOIN identifiers ON identifiers.type = k.type::identifier_type
  AND identifiers.domain = k.domain
  AND identifiers.value = k.value
JOIN instruments ON instruments.id = identifiers.instrument_id
ORDER BY instruments.id, identifiers.type, identifiers.domain, identifiers.value;

-- name: LockIdentifiers :exec
-- Serialises creation on the identifiers a stated key admits, for the
-- transaction. The caller passes them sorted, so two transactions take the
-- locks in one order. The seed keeps the keys apart from LockUserKeys.
SELECT pg_advisory_xact_lock(hashtextextended(k, 1)) FROM unnest(@keys::text[]) AS k;

-- name: DeferConstraints :exec
SET CONSTRAINTS ALL DEFERRED;

-- name: MoveListing :exec
UPDATE listings SET instrument_id = @instrument_id WHERE id = @id;

-- name: RelinkIdentifiers :exec
-- Moves the identifiers of loser onto survivor, each listing grain one onto
-- survivor's listing of its family.
UPDATE identifiers
SET instrument_id = @survivor::uuid,
    listing_id = (SELECT s.id FROM listings s
                  JOIN listings l ON l.currency = s.currency
                  WHERE l.id = identifiers.listing_id AND s.instrument_id = @survivor::uuid)
WHERE instrument_id = @loser::uuid;

-- name: RelinkStatedKeys :exec
-- Moves the stated keys of loser onto survivor, each listing onto survivor's
-- listing of its family.
UPDATE stated_keys
SET instrument_id = @survivor::uuid,
    listing_id = (SELECT s.id FROM listings s
                  JOIN listings l ON l.currency = s.currency
                  WHERE l.id = stated_keys.listing_id AND s.instrument_id = @survivor::uuid)
WHERE instrument_id = @loser::uuid;

-- name: DeleteListings :exec
DELETE FROM listings WHERE instrument_id = $1;

-- name: DeleteInstrument :exec
DELETE FROM instruments WHERE id = $1;

-- name: GetListing :one
SELECT * FROM listings
WHERE instrument_id = $1 AND currency = $2;

-- name: ListResolutionItems :many
-- A NULL after starts at the first item, and a NULL lim reads every item.
SELECT sqlc.embed(resolution_keys), sqlc.embed(stated_keys)
FROM resolution_keys
JOIN stated_keys ON stated_keys.id = resolution_keys.stated_key_id
WHERE resolution_keys.run_id = @run_id
  AND (sqlc.narg(after)::uuid IS NULL OR resolution_keys.stated_key_id > sqlc.narg(after))
ORDER BY resolution_keys.stated_key_id
LIMIT sqlc.narg(lim)::int;

-- name: ListInstruments :many
-- Every instrument with the identifier that names it, ordered by that name's
-- value, then id. An instrument with no identifier has an empty name and
-- type. q matches any of an instrument's identifiers, and asset_class
-- includes the classes below it. A page continues past the name and id of
-- the last instrument served, whether or not that row remains.
WITH RECURSIVE classes AS (
    SELECT class FROM asset_class_tree WHERE class = sqlc.narg(asset_class)::asset_class
    UNION ALL
    SELECT asset_class_tree.class FROM asset_class_tree
    JOIN classes ON asset_class_tree.parent = classes.class
), named AS (
    SELECT instruments.id, instruments.asset_class,
           COALESCE(instrument_names.value, '')::text AS name,
           COALESCE(instrument_names.type::text, '')::text AS name_type,
           COALESCE(instrument_names.domain, '')::text AS name_domain
    FROM instruments
    LEFT JOIN instrument_names ON instrument_names.instrument_id = instruments.id
    WHERE (sqlc.narg(q)::text IS NULL
           OR EXISTS (SELECT 1 FROM identifiers
                      WHERE identifiers.instrument_id = instruments.id
                        AND identifiers.value ILIKE '%' || sqlc.narg(q) || '%'))
      AND (sqlc.narg(asset_class)::asset_class IS NULL
           OR instruments.asset_class IN (SELECT class FROM classes))
)
SELECT * FROM named
WHERE sqlc.narg(after_id)::uuid IS NULL
   OR (name, id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid)
ORDER BY name, id
LIMIT @lim;
