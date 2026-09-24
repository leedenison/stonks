-- name: ListDatasources :many
SELECT * FROM datasources ORDER BY precedence, name;

-- name: CreateDatasource :one
INSERT INTO datasources (name, enabled, precedence, credential, endpoint)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateFetch :one
INSERT INTO fetches (id, user_id, datasource, kind)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetFetch :one
SELECT * FROM fetches WHERE id = $1 AND user_id = $2;

-- name: CreateFetchKey :exec
INSERT INTO fetch_keys (id, fetch_id, user_id, stated_key_id, outcome, attempts,
    sent_type, sent_domain, sent_value, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

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

-- name: ClearDatasourceBlock :exec
WITH block AS (
    UPDATE datasource_blocks SET cleared_at = now()
    WHERE datasource_blocks.id = $1 AND cleared_at IS NULL
    RETURNING id
)
UPDATE findings SET cleared_at = now()
FROM block WHERE findings.block_id = block.id;

-- name: ListFetchItems :many
SELECT sqlc.embed(fetch_keys), sqlc.embed(stated_keys)
FROM fetch_keys
JOIN stated_keys ON stated_keys.id = fetch_keys.stated_key_id
WHERE fetch_keys.fetch_id = $1
ORDER BY fetch_keys.id;
