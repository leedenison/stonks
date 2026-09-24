-- name: ListRunFindings :many
SELECT * FROM findings WHERE run_id = $1 ORDER BY id;

-- name: ListFindings :many
SELECT * FROM findings
WHERE (@include_cleared::bool OR cleared_at IS NULL)
  AND (sqlc.narg(run_id)::uuid IS NULL OR run_id = sqlc.narg(run_id))
  AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before))
ORDER BY id DESC
LIMIT @lim;

-- name: GetFinding :one
SELECT * FROM findings WHERE id = $1;

-- name: ClearFinding :exec
UPDATE findings SET cleared_at = now()
WHERE id = $1 AND block_id IS NULL AND cleared_at IS NULL;
