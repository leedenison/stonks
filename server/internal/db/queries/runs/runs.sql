-- name: CreateRun :one
INSERT INTO runs (id, user_id, kind, trigger, parent_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1 AND user_id = $2;

-- name: StartRun :one
UPDATE runs SET state = 'running', started_at = now()
WHERE id = $1 AND state = 'pending'
RETURNING *;

-- name: CompleteRun :exec
UPDATE runs SET state = 'completed', finished_at = now()
WHERE id = $1 AND state = 'running';

-- name: FailRun :exec
UPDATE runs SET state = 'failed', error = @error::text, finished_at = now()
WHERE id = $1 AND state = 'running';

-- name: InterruptRuns :execrows
UPDATE runs SET state = 'interrupted', finished_at = now()
WHERE state IN ('pending', 'running');

-- name: ListChildRuns :many
SELECT * FROM runs
WHERE parent_id = $1 AND user_id = $2
ORDER BY id;
