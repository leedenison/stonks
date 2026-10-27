-- name: ListUnavailableKeys :many
-- The source run is an ingest statement run, which supplies the keys its
-- statement states, or a resolve statement keys run, which supplies the keys it
-- resolved. Run ids order by creation, so the latest resolution of a key is its
-- row with the greatest run id.
SELECT sqlc.embed(stated_keys)
FROM stated_keys
JOIN LATERAL (
    SELECT outcome FROM resolution_keys
    WHERE resolution_keys.stated_key_id = stated_keys.id
      AND resolution_keys.user_id = stated_keys.user_id
    ORDER BY resolution_keys.run_id DESC
    LIMIT 1
) latest ON true
WHERE stated_keys.user_id = @user_id::uuid
  AND (EXISTS (SELECT 1 FROM stmt_keys
               WHERE stmt_keys.stmt_id = @source_id::uuid
                 AND stmt_keys.user_id = stated_keys.user_id
                 AND stmt_keys.stated_key_id = stated_keys.id)
       OR EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @source_id::uuid
                    AND resolution_keys.user_id = stated_keys.user_id
                    AND resolution_keys.stated_key_id = stated_keys.id))
  AND latest.outcome = 'unavailable'
  AND EXISTS (SELECT 1 FROM txns
              WHERE txns.user_id = stated_keys.user_id
                AND txns.stated_key_id = stated_keys.id)
ORDER BY stated_keys.id;

-- name: ListKeysUncoveredBy :many
-- The source is read as in ListUnavailableKeys. Keys of reference data are
-- left out: every datasource covers it without a row. A key the user
-- arbitrated is left out: a replay never changes its association. A dated
-- key is left out: it is associated, and its transactions read the facts.
SELECT sqlc.embed(stated_keys)
FROM stated_keys
LEFT JOIN instrs ON instrs.id = stated_keys.instr_id
WHERE stated_keys.user_id = @user_id::uuid
  AND (EXISTS (SELECT 1 FROM stmt_keys
               WHERE stmt_keys.stmt_id = @source_id::uuid
                 AND stmt_keys.user_id = stated_keys.user_id
                 AND stmt_keys.stated_key_id = stated_keys.id)
       OR EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @source_id::uuid
                    AND resolution_keys.user_id = stated_keys.user_id
                    AND resolution_keys.stated_key_id = stated_keys.id))
  AND EXISTS (SELECT 1 FROM txns
              WHERE txns.user_id = stated_keys.user_id
                AND txns.stated_key_id = stated_keys.id)
  AND stated_keys.arbiter IS DISTINCT FROM 'user'
  AND stated_keys.via_ticker_id IS NULL
  AND (stated_keys.instr_id IS NULL
       OR (instrs.fetch_key_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1 FROM identity_coverage
               WHERE identity_coverage.instr_id = stated_keys.instr_id
                 AND identity_coverage.datasource = @datasource::text)))
ORDER BY stated_keys.id;

-- name: CreateReplay :exec
INSERT INTO replays (id, user_id, source_id, datasource, started_by)
VALUES ($1, $2, $3, $4, $5);

-- name: GetReplay :one
SELECT sqlc.embed(replays), users.email AS started_by_email
FROM replays
JOIN users ON users.id = replays.started_by
WHERE replays.id = $1;
