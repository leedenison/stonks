-- name: CreateStmt :one
INSERT INTO stmts (id, user_id, broker, order_from, order_before, row_count)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStmt :one
SELECT sqlc.embed(stmts), sqlc.embed(runs),
       (SELECT count(*) FROM stmt_items WHERE stmt_items.stmt_id = stmts.id)::int AS rejected
FROM stmts
JOIN runs ON runs.id = stmts.id
WHERE stmts.id = $1 AND stmts.user_id = $2;

-- name: ListStmts :many
SELECT sqlc.embed(stmts), sqlc.embed(runs),
       (SELECT count(*) FROM stmt_items WHERE stmt_items.stmt_id = stmts.id)::int AS rejected
FROM stmts
JOIN runs ON runs.id = stmts.id
WHERE stmts.user_id = $1
ORDER BY stmts.id DESC;

-- name: CreateStmtItem :exec
INSERT INTO stmt_items (stmt_id, user_id, ordinal, reason, stated)
VALUES ($1, $2, $3, $4, $5);

-- name: CreateStmtItems :copyfrom
INSERT INTO stmt_items (stmt_id, user_id, ordinal, reason, stated)
VALUES ($1, $2, $3, $4, $5);

-- name: ListStmtItems :many
-- A NULL after starts at the first item, and a NULL lim reads every item.
SELECT * FROM stmt_items
WHERE stmt_id = @stmt_id AND user_id = @user_id
  AND (sqlc.narg(after)::int IS NULL OR ordinal > sqlc.narg(after))
ORDER BY ordinal
LIMIT sqlc.narg(lim)::int;

-- name: CreateStmtSplit :exec
INSERT INTO stmt_splits (stmt_id, user_id, ordinal, stated_key_id, effective_date, quantity, ratio_from, ratio_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
