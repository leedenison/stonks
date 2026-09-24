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

-- name: InterruptRuns :many
UPDATE runs SET state = 'interrupted', finished_at = now()
WHERE state IN ('pending', 'running')
RETURNING kind, trigger;

-- name: ListChildRuns :many
SELECT * FROM runs
WHERE parent_id = $1 AND user_id = $2
ORDER BY id;

-- name: ListUserRuns :many
SELECT sqlc.embed(runs), users.email
FROM runs
JOIN users ON users.id = runs.user_id
WHERE (sqlc.narg(kind)::run_kind IS NULL OR runs.kind = sqlc.narg(kind))
  AND (sqlc.narg(trigger)::run_trigger IS NULL OR runs.trigger = sqlc.narg(trigger))
  AND (sqlc.narg(state)::run_state IS NULL OR runs.state = sqlc.narg(state))
  AND (sqlc.narg(user_id)::uuid IS NULL OR runs.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(before)::uuid IS NULL OR runs.id < sqlc.narg(before))
ORDER BY runs.id DESC
LIMIT @lim;

-- name: GetUserRun :one
SELECT sqlc.embed(runs), users.email
FROM runs
JOIN users ON users.id = runs.user_id
WHERE runs.id = $1;
