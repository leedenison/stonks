-- name: ListUnavailableKeys :many
-- The source run is a statement run, which supplies its statement's keys, or
-- a resolution run, which supplies the keys it resolved. Run ids order by
-- creation, so the latest resolution of a key is its row with the greatest
-- run id.
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
  AND (stated_keys.statement_id = @source_id::uuid
       OR EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @source_id::uuid
                    AND resolution_keys.user_id = stated_keys.user_id
                    AND resolution_keys.stated_key_id = stated_keys.id))
  AND latest.outcome = 'unavailable'
  AND EXISTS (SELECT 1 FROM transactions
              WHERE transactions.user_id = stated_keys.user_id
                AND transactions.stated_key_id = stated_keys.id)
ORDER BY stated_keys.id;

-- name: ListKeysUncoveredBy :many
-- The source is read as in ListUnavailableKeys. Keys of reference data are
-- left out: every datasource covers it without a row.
SELECT sqlc.embed(stated_keys)
FROM stated_keys
LEFT JOIN instruments ON instruments.id = stated_keys.instrument_id
WHERE stated_keys.user_id = @user_id::uuid
  AND (stated_keys.statement_id = @source_id::uuid
       OR EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @source_id::uuid
                    AND resolution_keys.user_id = stated_keys.user_id
                    AND resolution_keys.stated_key_id = stated_keys.id))
  AND EXISTS (SELECT 1 FROM transactions
              WHERE transactions.user_id = stated_keys.user_id
                AND transactions.stated_key_id = stated_keys.id)
  AND (stated_keys.instrument_id IS NULL
       OR (instruments.fetch_key_id IS NOT NULL
           AND NOT EXISTS (
               SELECT 1 FROM identity_coverage
               WHERE identity_coverage.instrument_id = stated_keys.instrument_id
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
