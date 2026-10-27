-- name: CreateInstr :one
INSERT INTO instrs (id, asset_class, fetch_key_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateListing :one
INSERT INTO listings (id, instr_id, currency, fetch_key_id, primary_mic)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: SetListingPrimary :exec
-- Sets the primary venue of a listing that has none.
UPDATE listings SET primary_mic = @primary_mic WHERE id = @id AND primary_mic IS NULL;

-- name: ListListingsOf :many
SELECT * FROM listings
WHERE instr_id = ANY(@ids::uuid[])
ORDER BY instr_id, currency;

-- name: ListIdentifiersOf :many
-- By the rank of the type as a name, then by domain and value, which is the
-- order the API states.
SELECT identifiers.* FROM identifiers
JOIN identifier_type_traits ON identifier_type_traits.type = identifiers.type
WHERE identifiers.instr_id = ANY(@ids::uuid[])
ORDER BY identifiers.instr_id, identifier_type_traits.name_rank, identifiers.domain, identifiers.value;

-- name: GetInstr :one
SELECT * FROM instrs WHERE id = $1;

-- name: ListInstrsByID :many
SELECT * FROM instrs WHERE id = ANY(@ids::uuid[]) ORDER BY id;

-- name: ListInstrNames :many
SELECT * FROM instr_names
WHERE instr_id = ANY(@ids::uuid[])
ORDER BY instr_id;

-- name: ListViaOf :many
-- The ids of the via identifiers recorded on the stated keys of the
-- instruments.
SELECT DISTINCT stated_keys.via_identifier_id::uuid AS id FROM stated_keys
WHERE stated_keys.instr_id = ANY(@ids::uuid[]) AND stated_keys.via_identifier_id IS NOT NULL
ORDER BY id;

-- name: ListListings :many
SELECT * FROM listings
WHERE instr_id = $1
ORDER BY currency;

-- name: CreateIdentifier :one
INSERT INTO identifiers (id, instr_id, listing_id, type, domain, value, fetch_key_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: PlaceTicker :one
-- Writes a ticker onto a listing of its present holder. Where another
-- instrument carries the ticker, the row moves to this one, with the fetch
-- key that placed it.
INSERT INTO identifiers (id, instr_id, listing_id, type, domain, value, fetch_key_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (type, domain, value) DO UPDATE
SET instr_id = EXCLUDED.instr_id, listing_id = EXCLUDED.listing_id,
    fetch_key_id = EXCLUDED.fetch_key_id
RETURNING *;

-- name: ListIdentifiers :many
SELECT * FROM identifiers
WHERE instr_id = $1
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
SELECT sqlc.embed(identifiers), sqlc.embed(instrs)
FROM identifiers
JOIN instrs ON instrs.id = identifiers.instr_id
WHERE identifiers.type = @type
  AND identifiers.domain = @domain::text
  AND identifiers.value = @value::text;

-- name: ListInstrsByIdentifiers :many
-- The cast sits on the parameter, so the unique index on identifiers serves the
-- join.
SELECT sqlc.embed(identifiers), sqlc.embed(instrs)
FROM (SELECT unnest(@types::text[]) AS type, unnest(@domains::text[]) AS domain,
             unnest(@values::text[]) AS value) AS k
JOIN identifiers ON identifiers.type = k.type::identifier_type
  AND identifiers.domain = k.domain
  AND identifiers.value = k.value
JOIN instrs ON instrs.id = identifiers.instr_id
ORDER BY instrs.id, identifiers.type, identifiers.domain, identifiers.value;

-- name: LockAdvisory :exec
-- Takes the transaction's advisory lock on each name. The caller passes the
-- names sorted, so two transactions take the locks in one order. The seed
-- keeps these locks apart from LockUserKeys.
SELECT pg_advisory_xact_lock(hashtextextended(n, 1)) FROM unnest(@names::text[]) AS n;

-- name: DeferConstraints :exec
SET CONSTRAINTS ALL DEFERRED;

-- name: MoveListing :exec
UPDATE listings SET instr_id = @instr_id WHERE id = @id;

-- name: RelinkIdentifiers :exec
-- Moves the identifiers of loser onto survivor, each listing grain one onto
-- survivor's listing of its family.
UPDATE identifiers
SET instr_id = @survivor::uuid,
    listing_id = (SELECT s.id FROM listings s
                  JOIN listings l ON l.currency = s.currency
                  WHERE l.id = identifiers.listing_id AND s.instr_id = @survivor::uuid)
WHERE instr_id = @loser::uuid;

-- name: RelinkStatedKeys :exec
-- Moves the stated keys of loser onto survivor, each listing onto survivor's
-- listing of its family.
UPDATE stated_keys
SET instr_id = @survivor::uuid,
    listing_id = (SELECT s.id FROM listings s
                  JOIN listings l ON l.currency = s.currency
                  WHERE l.id = stated_keys.listing_id AND s.instr_id = @survivor::uuid)
WHERE instr_id = @loser::uuid;

-- name: DeleteListings :exec
DELETE FROM listings WHERE instr_id = $1;

-- name: DeleteInstr :exec
DELETE FROM instrs WHERE id = $1;

-- name: GetListing :one
SELECT * FROM listings
WHERE instr_id = $1 AND currency = $2;

-- name: ListResolutionItems :many
-- A NULL after starts at the first item, and a NULL lim reads every item.
SELECT sqlc.embed(resolution_keys), sqlc.embed(stated_keys)
FROM resolution_keys
JOIN stated_keys ON stated_keys.id = resolution_keys.stated_key_id
WHERE resolution_keys.run_id = @run_id
  AND (sqlc.narg(after)::uuid IS NULL OR resolution_keys.stated_key_id > sqlc.narg(after))
ORDER BY resolution_keys.stated_key_id
LIMIT sqlc.narg(lim)::int;

-- name: ListInstrs :many
-- Every instrument with the identifier that names it, ordered by that name's
-- value, then id. An instrument without a name has an empty name and type. q
-- matches any of an instrument's identifiers and the tickers of its rows of
-- resolved_tickers. asset_class includes the classes below it. A page
-- continues past the name and id of the last instrument served, whether or
-- not that row remains.
WITH RECURSIVE classes AS (
    SELECT class FROM asset_class_tree WHERE class = sqlc.narg(asset_class)::asset_class
    UNION ALL
    SELECT asset_class_tree.class FROM asset_class_tree
    JOIN classes ON asset_class_tree.parent = classes.class
), named AS (
    SELECT instrs.id, instrs.asset_class,
           COALESCE(instr_names.value, '')::text AS name,
           COALESCE(instr_names.type::text, '')::text AS name_type,
           COALESCE(instr_names.domain, '')::text AS name_domain
    FROM instrs
    LEFT JOIN instr_names ON instr_names.instr_id = instrs.id
    WHERE (sqlc.narg(q)::text IS NULL
           OR EXISTS (SELECT 1 FROM identifiers
                      WHERE identifiers.instr_id = instrs.id
                        AND identifiers.value ILIKE '%' || sqlc.narg(q) || '%')
           OR EXISTS (SELECT 1 FROM resolved_tickers
                      JOIN tickers ON tickers.id = resolved_tickers.ticker_id
                      WHERE resolved_tickers.instr_id = instrs.id
                        AND tickers.value ILIKE '%' || sqlc.narg(q) || '%'))
      AND (sqlc.narg(asset_class)::asset_class IS NULL
           OR instrs.asset_class IN (SELECT class FROM classes))
)
SELECT * FROM named
WHERE sqlc.narg(after_id)::uuid IS NULL
   OR (name, id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid)
ORDER BY name, id
LIMIT @lim;
