-- name: CreateFinding :exec
INSERT INTO findings (id, run_id, kind, stated_key_id, fetch_key_id, step, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListRunFindings :many
-- The findings of the runs, oldest first, each with what the key concerned
-- states: the finding's own key, or for a block the key whose call created
-- it. The key columns are null where no key is concerned. A block finding
-- carries its block's reason.
SELECT sqlc.embed(findings), datasource_blocks.reason AS block_reason,
       stated_keys.id AS key_id, stated_keys.identifiers AS key_identifiers,
       stated_keys.asset_class AS key_asset_class, stated_keys.currency AS key_currency,
       stated_keys.description AS key_description
FROM findings
LEFT JOIN datasource_blocks ON datasource_blocks.id = findings.block_id
LEFT JOIN fetch_keys ON fetch_keys.id = datasource_blocks.fetch_key_id
LEFT JOIN stated_keys ON stated_keys.id = coalesce(findings.stated_key_id, fetch_keys.stated_key_id)
WHERE findings.run_id = ANY(@run_ids::uuid[])
ORDER BY findings.id;

-- name: GetFinding :one
SELECT * FROM findings WHERE id = $1;

-- name: ClearFinding :exec
UPDATE findings SET cleared_at = now()
WHERE id = $1 AND block_id IS NULL AND cleared_at IS NULL;
