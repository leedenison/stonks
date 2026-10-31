-- name: CreateRun :one
INSERT INTO runs (id, user_id, kind, trigger, parent_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1 AND user_id = $2;

-- name: GetRunByID :one
SELECT * FROM runs WHERE id = $1;

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
-- A restart ends every follow-up of a statement, so this also settles every
-- unsettled statement.
WITH settled AS (
    UPDATE stmts SET settled_at = now() WHERE settled_at IS NULL
)
UPDATE runs SET state = 'interrupted', finished_at = now()
WHERE state IN ('pending', 'running')
RETURNING kind, trigger;

-- name: ListRunAncestors :many
-- The runs above a run, the top-level run first.
WITH RECURSIVE up AS (
    SELECT runs.parent_id AS id, 1 AS depth FROM runs WHERE runs.id = $1
    UNION ALL
    SELECT runs.parent_id AS id, up.depth + 1 AS depth FROM runs JOIN up ON runs.id = up.id
)
SELECT sqlc.embed(runs), sqlc.embed(run_details)
FROM up
JOIN runs ON runs.id = up.id
JOIN run_details ON run_details.run_id = runs.id
ORDER BY up.depth DESC;

-- name: ListRunDescendants :many
-- The runs below a run, every parent before its children.
WITH RECURSIVE down AS (
    SELECT runs.id, 1 AS depth FROM runs WHERE runs.parent_id = $1
    UNION ALL
    SELECT runs.id, down.depth + 1 AS depth FROM runs JOIN down ON runs.parent_id = down.id
)
SELECT sqlc.embed(runs), sqlc.embed(run_details)
FROM down
JOIN runs ON runs.id = down.id
JOIN run_details ON run_details.run_id = runs.id
ORDER BY down.depth, runs.id;

-- name: ListChildRuns :many
SELECT * FROM runs
WHERE parent_id = $1 AND user_id = $2
ORDER BY id;

-- name: ListRootRuns :many
-- The top-level runs, newest first, each followed by the runs below it,
-- oldest first. The columns are those of ListUserRuns, with matched true for
-- every row.
WITH RECURSIVE roots AS (
    SELECT id FROM runs
    WHERE parent_id IS NULL AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before))
    ORDER BY id DESC
    LIMIT @lim
), tree AS (
    SELECT id, id AS root_id FROM roots
    UNION ALL
    SELECT runs.id, tree.root_id FROM runs JOIN tree ON runs.parent_id = tree.id
)
SELECT sqlc.embed(runs), sqlc.embed(run_details), tree.root_id, true::bool AS matched
FROM tree
JOIN runs ON runs.id = tree.id
JOIN run_details ON run_details.run_id = runs.id
ORDER BY tree.root_id DESC, runs.id;

-- name: ListUserRuns :many
-- The runs matching the filters and the ancestors leading to each, grouped
-- under their top-level runs, newest top-level run first and oldest run
-- first within one. A page holds lim top-level runs, and before pages by
-- them.
WITH RECURSIVE matched AS (
    SELECT id, parent_id FROM runs
    WHERE (sqlc.narg(kind)::run_kind IS NULL OR kind = sqlc.narg(kind))
      AND (sqlc.narg(trigger)::run_trigger IS NULL OR trigger = sqlc.narg(trigger))
      AND (sqlc.narg(state)::run_state IS NULL OR state = sqlc.narg(state))
      AND (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id))
), lineage AS (
    SELECT id, parent_id FROM matched
    UNION
    SELECT runs.id, runs.parent_id FROM runs JOIN lineage ON runs.id = lineage.parent_id
), roots AS (
    SELECT id FROM lineage
    WHERE parent_id IS NULL AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before))
    ORDER BY id DESC
    LIMIT @lim
), tree AS (
    SELECT id, id AS root_id FROM roots
    UNION ALL
    SELECT runs.id, tree.root_id FROM runs JOIN tree ON runs.parent_id = tree.id
)
SELECT sqlc.embed(runs), sqlc.embed(run_details), tree.root_id, (runs.id IN (SELECT id FROM matched))::bool AS matched
FROM tree
JOIN runs ON runs.id = tree.id
JOIN run_details ON run_details.run_id = runs.id
WHERE runs.id IN (SELECT id FROM lineage)
ORDER BY tree.root_id DESC, runs.id;

-- name: GetUserRun :one
SELECT sqlc.embed(runs), sqlc.embed(run_details)
FROM runs
JOIN run_details ON run_details.run_id = runs.id
WHERE runs.id = $1;

-- name: ListFailedRuns :many
-- The uncleared runs that failed or were interrupted, newest first, with the
-- columns of GetUserRun. A NULL before starts at the newest.
SELECT sqlc.embed(runs), sqlc.embed(run_details)
FROM runs
JOIN run_details ON run_details.run_id = runs.id
WHERE runs.state IN ('failed', 'interrupted') AND runs.cleared_at IS NULL
  AND (sqlc.narg(before)::uuid IS NULL OR runs.id < sqlc.narg(before))
ORDER BY runs.id DESC
LIMIT @lim;

-- name: CountFailedRuns :one
SELECT count(*)::int FROM runs WHERE state IN ('failed', 'interrupted') AND cleared_at IS NULL;

-- name: ClearRun :one
-- Clears a failed or interrupted run and returns the run's state, so the
-- caller can refuse any other state. Clearing a cleared run keeps the first
-- time.
UPDATE runs
SET cleared_at = CASE WHEN state IN ('failed', 'interrupted') THEN coalesce(cleared_at, now()) ELSE cleared_at END
WHERE id = $1
RETURNING state;
