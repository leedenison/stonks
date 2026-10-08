-- name: ListDatasources :many
SELECT * FROM datasources ORDER BY precedence, name;

-- name: CreateDatasource :one
-- A NULL config is the empty object.
INSERT INTO datasources (name, enabled, precedence, credential, endpoint, config)
VALUES (@name, @enabled, @precedence, sqlc.narg(credential), sqlc.narg(endpoint),
    COALESCE(sqlc.narg(config)::jsonb, '{}'))
RETURNING *;

-- name: CreateFetch :one
INSERT INTO fetches (id, user_id, datasource, kind)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetFetch :one
SELECT * FROM fetches WHERE id = $1 AND user_id = $2;

-- name: CreateFetchKey :exec
INSERT INTO fetch_keys (id, fetch_id, user_id, stated_key_id, outcome, attempts,
    sent_type, sent_domain, sent_value, reason, candidates)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ListFetchKeys :many
SELECT * FROM fetch_keys WHERE fetch_id = $1 AND user_id = $2 ORDER BY id;

-- name: SetFetchKeyInstrument :exec
UPDATE fetch_keys SET instrument_id = $3 WHERE id = $1 AND user_id = $2;

-- name: CreateFetchIdentifier :exec
INSERT INTO fetch_identifiers (fetch_key_id, type, domain, value)
VALUES ($1, $2, $3, $4);

-- name: ListFetchIdentifiers :many
SELECT * FROM fetch_identifiers WHERE fetch_key_id = $1
ORDER BY type, domain, value;

-- name: ListIdentityCoverage :many
SELECT * FROM identity_coverage
WHERE instrument_id = ANY(@instrument_ids::uuid[])
ORDER BY instrument_id, datasource;

-- name: UpsertIdentityCoverage :exec
INSERT INTO identity_coverage (instrument_id, datasource, fetch_key_id)
VALUES ($1, $2, $3)
ON CONFLICT (instrument_id, datasource) DO UPDATE
SET fetch_key_id = EXCLUDED.fetch_key_id, covered_at = now();

-- name: RelinkFetchKeys :exec
UPDATE fetch_keys SET instrument_id = @survivor::uuid WHERE instrument_id = @loser::uuid;

-- name: MoveIdentityCoverage :exec
INSERT INTO identity_coverage (instrument_id, datasource, fetch_key_id, covered_at)
SELECT @survivor::uuid, datasource, fetch_key_id, covered_at
FROM identity_coverage WHERE instrument_id = @loser::uuid
ON CONFLICT (instrument_id, datasource) DO NOTHING;

-- name: DeleteIdentityCoverage :exec
DELETE FROM identity_coverage WHERE instrument_id = $1;

-- name: ListOpenBlocks :many
SELECT * FROM datasource_blocks
WHERE datasource = $1 AND kind = $2 AND cleared_at IS NULL
ORDER BY id;

-- name: CreateDatasourceBlock :execrows
-- The block and the finding reporting it are written together, and neither is
-- written while an open block of the same scope exists.
WITH block AS (
    INSERT INTO datasource_blocks (id, datasource, kind, scope, sent_type, sent_domain,
        sent_value, reason, fetch_key_id)
    VALUES (@id, @datasource, @kind, @scope, @sent_type, @sent_domain, @sent_value,
        @reason, @fetch_key_id)
    ON CONFLICT DO NOTHING
    RETURNING id
)
INSERT INTO findings (id, run_id, kind, block_id)
SELECT @finding_id, @run_id, 'block', block.id FROM block;

-- name: ClearDatasourceBlock :one
-- The block and the finding reporting it are cleared together. Clearing a
-- cleared block keeps the time it was first cleared.
WITH block AS (
    UPDATE datasource_blocks SET cleared_at = coalesce(cleared_at, now())
    WHERE datasource_blocks.id = $1
    RETURNING id
), finding AS (
    UPDATE findings SET cleared_at = coalesce(findings.cleared_at, now())
    FROM block WHERE findings.block_id = block.id
    RETURNING findings.id
)
SELECT id FROM block;

-- name: ListFetchItems :many
-- A NULL after starts at the first item, and a NULL lim reads every item.
SELECT sqlc.embed(fetch_keys), sqlc.embed(stated_keys)
FROM fetch_keys
JOIN stated_keys ON stated_keys.id = fetch_keys.stated_key_id
WHERE fetch_keys.fetch_id = @fetch_id
  AND (sqlc.narg(after)::uuid IS NULL OR fetch_keys.stated_key_id > sqlc.narg(after))
ORDER BY fetch_keys.stated_key_id
LIMIT sqlc.narg(lim)::int;

-- name: ListDatasourceSettings :many
SELECT name, enabled, precedence, endpoint, (credential IS NOT NULL)::bool AS has_credential, config
FROM datasources
ORDER BY precedence, name;

-- name: UpdateDatasource :one
-- A NULL endpoint, credential or config keeps the one held. An empty
-- endpoint or credential clears it.
UPDATE datasources
SET enabled = @enabled,
    endpoint = CASE WHEN sqlc.narg(endpoint)::text IS NULL THEN endpoint
                    WHEN sqlc.narg(endpoint)::text = '' THEN NULL
                    ELSE sqlc.narg(endpoint)::text END,
    credential = CASE WHEN sqlc.narg(credential)::text IS NULL THEN credential
                      WHEN sqlc.narg(credential)::text = '' THEN NULL
                      ELSE sqlc.narg(credential)::text END,
    config = COALESCE(sqlc.narg(config)::jsonb, config)
WHERE name = @name
RETURNING *;

-- name: SetDatasourcePrecedence :exec
-- Each datasource takes its position in names, from 1.
UPDATE datasources SET precedence = v.position
FROM unnest(@names::text[]) WITH ORDINALITY AS v(name, position)
WHERE datasources.name = v.name;

-- name: ListBlocks :many
SELECT sqlc.embed(datasource_blocks), fetch_keys.fetch_id
FROM datasource_blocks
JOIN fetch_keys ON fetch_keys.id = datasource_blocks.fetch_key_id
WHERE (@include_cleared::bool OR datasource_blocks.cleared_at IS NULL)
  AND (sqlc.narg(before)::uuid IS NULL OR datasource_blocks.id < sqlc.narg(before))
ORDER BY datasource_blocks.id DESC
LIMIT @lim;

