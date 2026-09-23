-- name: CreateStatedKey :one
INSERT INTO stated_keys (id, statement_id, user_id, asset_class, currency, description, identifiers)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListStatedKeys :many
SELECT * FROM stated_keys
WHERE statement_id = $1 AND user_id = $2
ORDER BY id;

-- name: CreateTransaction :one
INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id,
                          order_date, settlement_date, as_at, quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: DeleteTransactions :execrows
DELETE FROM transactions
WHERE user_id = $1 AND broker = $2
  AND order_date >= @order_from::date AND order_date < @order_before::date;

-- name: ListTransactions :many
SELECT * FROM transactions
WHERE user_id = $1
ORDER BY order_date, id;

-- name: SetStatedKeyAssociation :exec
UPDATE stated_keys
SET instrument_id = $3, listing_id = $4, via_id = $5, validity = $6
WHERE id = $1 AND user_id = $2;

-- name: LockUserKeys :exec
SELECT pg_advisory_xact_lock(hashtextextended(CAST(@user_id::uuid AS text), 0));

-- name: ListGroupableKeys :many
SELECT sqlc.embed(stated_keys), statements.broker
FROM stated_keys
JOIN statements ON statements.id = stated_keys.statement_id
WHERE stated_keys.user_id = @user_id::uuid
  AND stated_keys.instrument_id IS NULL
  AND EXISTS (
      SELECT 1 FROM transactions
      WHERE transactions.user_id = stated_keys.user_id
        AND transactions.stated_key_id = stated_keys.id)
ORDER BY stated_keys.id;

-- name: ClearStatedKeyGroups :exec
UPDATE stated_keys SET group_id = NULL
WHERE user_id = $1 AND group_id IS NOT NULL;

-- name: SetStatedKeyGroups :exec
UPDATE stated_keys SET group_id = v.group_id
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@group_ids::uuid[]) AS group_id) AS v
WHERE stated_keys.user_id = @user_id::uuid AND stated_keys.id = v.id;
