-- name: UpsertStatedKey :one
-- When the user's statements already state the key, the query returns the
-- existing row. The update changes nothing, so RETURNING yields that row and
-- the conflict path takes its lock. When two statements of one user state a new key at once, the
-- second waits on the first's uncommitted row and then takes it.
INSERT INTO stated_keys (id, user_id, asset_class, currency, identifiers)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, asset_class, currency, identifiers)
DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING *;

-- name: CreateStatementKeys :copyfrom
INSERT INTO statement_keys (statement_id, user_id, stated_key_id)
VALUES ($1, $2, $3);

-- name: ListStatedKeys :many
SELECT stated_keys.* FROM stated_keys
JOIN statement_keys ON statement_keys.stated_key_id = stated_keys.id
WHERE statement_keys.statement_id = $1 AND statement_keys.user_id = $2
ORDER BY stated_keys.id;

-- name: CreateTransaction :one
INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id,
                          order_date, settlement_date, as_at, quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateTransactions :copyfrom
INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id,
                          order_date, settlement_date, as_at, quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: DeleteTransactions :execrows
DELETE FROM transactions
WHERE user_id = $1 AND broker = $2
  AND order_date >= @order_from::date AND order_date < @order_before::date;

-- name: SweepStatedKeys :execrows
-- Deletes the user's keys that lack both a transaction and a link to a
-- statement. The caller holds the user's key lock and has regrouped, so the
-- sweep leaves every group with its root.
DELETE FROM stated_keys
WHERE stated_keys.user_id = @user_id::uuid
  AND NOT EXISTS (SELECT 1 FROM transactions
                  WHERE transactions.user_id = stated_keys.user_id
                    AND transactions.stated_key_id = stated_keys.id)
  AND NOT EXISTS (SELECT 1 FROM statement_keys
                  WHERE statement_keys.stated_key_id = stated_keys.id);

-- name: ListTransactions :many
SELECT * FROM transactions
WHERE user_id = $1
ORDER BY order_date, id;

-- name: SetStatedKeyAssociation :execrows
-- Writes a fixed association. A key with a fixed association belongs to no
-- group. The update skips a key whose association the user arbitrated, and
-- so changes zero rows.
UPDATE stated_keys
SET instrument_id = $3, listing_id = $4, via_id = $5,
    via_type = NULL, via_domain = NULL, via_value = NULL,
    arbiter = @arbiter::arbiter, group_id = NULL
WHERE id = $1 AND user_id = $2 AND arbiter IS DISTINCT FROM 'user';

-- name: SetStatedKeyDatedAssociation :execrows
-- Writes a dated association through a MIC_TICKER. The key keeps its
-- group, which gathers the keys with no instrument that share an identifier.
-- The update skips a key whose association the user arbitrated.
UPDATE stated_keys
SET instrument_id = NULL, listing_id = NULL, via_id = NULL,
    via_type = 'mic_ticker', via_domain = @via_domain::text, via_value = @via_value::text,
    arbiter = @arbiter::arbiter
WHERE id = @id AND user_id = @user_id AND arbiter IS DISTINCT FROM 'user';

-- name: ListKeyOrderDates :many
-- The distinct order dates of the transactions of each of the user's keys.
SELECT DISTINCT stated_key_id, order_date FROM transactions
WHERE user_id = @user_id AND stated_key_id = ANY(@ids::uuid[])
ORDER BY stated_key_id, order_date;

-- name: ListUnassociatedKeysStating :many
-- Every user's keys that have no association and a transaction, and that
-- state one of the probes. A probe is a JSON array of one identifier object,
-- since containment matches an array against an array. A probe without a
-- value matches every key stating a ticker of its domain. The keys the run
-- resolved are left out.
SELECT * FROM stated_keys
WHERE stated_keys.instrument_id IS NULL AND stated_keys.via_type IS NULL
  AND stated_keys.identifiers @> ANY(@probes::jsonb[])
  AND NOT EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @run_id::uuid
                    AND resolution_keys.stated_key_id = stated_keys.id)
  AND EXISTS (SELECT 1 FROM transactions
              WHERE transactions.user_id = stated_keys.user_id
                AND transactions.stated_key_id = stated_keys.id)
ORDER BY stated_keys.user_id, stated_keys.id;

-- name: GetStatedKey :one
SELECT * FROM stated_keys WHERE id = $1 AND user_id = $2;

-- name: LockUserKeys :exec
-- The user's key lock: an advisory lock, held for the transaction, that
-- serialises the writes to one user's groups. A regroup takes it. A
-- resolution associates a single key without it. The lock's seed differs
-- from the seed of LockIdentifiers, so the two never collide.
SELECT pg_advisory_xact_lock(hashtextextended(CAST(@user_id::uuid AS text), 0));

-- name: ListGroupableKeys :many
SELECT * FROM stated_keys
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
-- Skips any key that a resolution associated after the caller listed it.
UPDATE stated_keys SET group_id = v.group_id
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@group_ids::uuid[]) AS group_id) AS v
WHERE stated_keys.user_id = @user_id::uuid AND stated_keys.id = v.id
  AND stated_keys.instrument_id IS NULL;
