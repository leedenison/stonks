-- name: CreateStatedKey :one
INSERT INTO stated_keys (id, statement_id, user_id, asset_class, currency, description, identifiers)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListStatedKeys :many
SELECT * FROM stated_keys
WHERE statement_id = $1 AND user_id = $2
ORDER BY id;

-- name: CreateTransaction :one
INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id, instrument_id, listing_id,
                          order_date, settlement_date, as_at, quantity, currency)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: DeleteTransactions :execrows
DELETE FROM transactions
WHERE user_id = $1 AND broker = $2
  AND order_date >= @order_from::date AND order_date < @order_before::date;

-- name: ListTransactions :many
SELECT * FROM transactions
WHERE user_id = $1
ORDER BY order_date, id;
