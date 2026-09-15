-- name: CreateStatement :one
INSERT INTO statements (id, user_id, broker, order_from, order_before, row_count)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStatement :one
SELECT sqlc.embed(statements), sqlc.embed(runs),
       (SELECT count(*) FROM statement_items WHERE statement_items.statement_id = statements.id)::int AS rejected
FROM statements
JOIN runs ON runs.id = statements.id
WHERE statements.id = $1 AND statements.user_id = $2;

-- name: ListStatements :many
SELECT sqlc.embed(statements), sqlc.embed(runs),
       (SELECT count(*) FROM statement_items WHERE statement_items.statement_id = statements.id)::int AS rejected
FROM statements
JOIN runs ON runs.id = statements.id
WHERE statements.user_id = $1
ORDER BY statements.id DESC;

-- name: CreateStatementItem :exec
INSERT INTO statement_items (statement_id, user_id, ordinal, reason, stated)
VALUES ($1, $2, $3, $4, $5);

-- name: ListStatementItems :many
SELECT * FROM statement_items
WHERE statement_id = $1 AND user_id = $2
ORDER BY ordinal;

-- name: CreateStatementSplit :exec
INSERT INTO statement_splits (statement_id, user_id, ordinal, stated_key_id, effective_date, quantity, ratio_from, ratio_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
