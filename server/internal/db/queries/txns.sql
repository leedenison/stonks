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

-- name: CreateStmtKeys :copyfrom
INSERT INTO stmt_keys (stmt_id, user_id, stated_key_id)
VALUES ($1, $2, $3);

-- name: ListStatedKeys :many
SELECT stated_keys.* FROM stated_keys
JOIN stmt_keys ON stmt_keys.stated_key_id = stated_keys.id
WHERE stmt_keys.stmt_id = $1 AND stmt_keys.user_id = $2
ORDER BY stated_keys.id;

-- name: CreateTxn :one
INSERT INTO txns (id, user_id, broker, stmt_id, stated_key_id,
                          order_date, settlement_date, as_at, quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateTxns :copyfrom
INSERT INTO txns (id, user_id, broker, stmt_id, stated_key_id,
                          order_date, settlement_date, as_at, quantity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: DeleteTxns :execrows
DELETE FROM txns
WHERE user_id = $1 AND broker = $2
  AND order_date >= @order_from::date AND order_date < @order_before::date;

-- name: SweepStatedKeys :execrows
-- Deletes the user's keys that lack both a transaction and a link to a
-- statement. The caller holds the user's key lock and has regrouped, so the
-- sweep leaves every group with its root.
DELETE FROM stated_keys
WHERE stated_keys.user_id = @user_id::uuid
  AND NOT EXISTS (SELECT 1 FROM txns
                  WHERE txns.user_id = stated_keys.user_id
                    AND txns.stated_key_id = stated_keys.id)
  AND NOT EXISTS (SELECT 1 FROM stmt_keys
                  WHERE stmt_keys.stated_key_id = stated_keys.id);

-- name: ListTxns :many
SELECT * FROM txns
WHERE user_id = $1
ORDER BY order_date, id;

-- name: SetStatedKeyAssociation :execrows
-- Writes a fixed association. A key with a fixed association belongs to no
-- group. The update skips a key whose association the user arbitrated, and
-- so changes zero rows.
UPDATE stated_keys
SET instr_id = $3, listing_id = $4, via_identifier_id = $5,
    via_ticker_id = NULL, via_market = NULL, via_venue = NULL,
    arbiter = @arbiter::arbiter, group_id = NULL
WHERE id = $1 AND user_id = $2 AND arbiter IS DISTINCT FROM 'user';

-- name: SetStatedKeyDatedAssociation :execrows
-- Writes a dated association through a MIC_TICKER. The key keeps its
-- group, which gathers the keys with no instrument that share an identifier.
-- The update skips a key whose association the user arbitrated.
UPDATE stated_keys
SET instr_id = NULL, listing_id = NULL, via_identifier_id = NULL,
    via_ticker_id = @via_ticker_id::uuid, via_market = @via_market::text, via_venue = @via_venue::text,
    arbiter = @arbiter::arbiter
WHERE id = @id AND user_id = @user_id AND arbiter IS DISTINCT FROM 'user';

-- name: ListStmtTxns :many
-- The statement's transactions, ordered by order date. Each carries how
-- firmly its key names an instrument on its as-at date.
SELECT sqlc.embed(txns), txn_instrs.validity
FROM txns
JOIN txn_instrs ON txn_instrs.txn_id = txns.id
WHERE txns.stmt_id = @stmt_id AND txns.user_id = @user_id
ORDER BY txns.order_date, txns.id;

-- name: ListKeyDates :many
-- The distinct as-at dates of the transactions of each of the user's keys.
SELECT DISTINCT stated_key_id, as_at FROM txns
WHERE user_id = @user_id AND stated_key_id = ANY(@ids::uuid[])
ORDER BY stated_key_id, as_at;

-- name: ListUnassociatedKeysStating :many
-- Every user's keys that have no association and a transaction, and that
-- state one of the probes. A probe is a JSON array of one identifier object,
-- since containment matches an array against an array. A probe without a
-- value matches every key stating a ticker of its domain. The keys the run
-- resolved are left out.
SELECT * FROM stated_keys
WHERE stated_keys.instr_id IS NULL AND stated_keys.via_ticker_id IS NULL
  AND stated_keys.identifiers @> ANY(@probes::jsonb[])
  AND NOT EXISTS (SELECT 1 FROM resolution_keys
                  WHERE resolution_keys.run_id = @run_id::uuid
                    AND resolution_keys.stated_key_id = stated_keys.id)
  AND EXISTS (SELECT 1 FROM txns
              WHERE txns.user_id = stated_keys.user_id
                AND txns.stated_key_id = stated_keys.id)
ORDER BY stated_keys.user_id, stated_keys.id;

-- name: GetStatedKey :one
SELECT * FROM stated_keys WHERE id = $1 AND user_id = $2;

-- name: LockUserKeys :exec
-- The user's key lock: an advisory lock, held for the transaction, that
-- serialises the writes to one user's groups. A regroup takes it. A
-- resolution associates a single key without it. The lock's seed differs
-- from the seed of LockAdvisory, so a collision between the two is unlikely.
SELECT pg_advisory_xact_lock(hashtextextended(CAST(@user_id::uuid AS text), 0));

-- name: ListGroupableKeys :many
SELECT * FROM stated_keys
WHERE stated_keys.user_id = @user_id::uuid
  AND stated_keys.instr_id IS NULL
  AND EXISTS (
      SELECT 1 FROM txns
      WHERE txns.user_id = stated_keys.user_id
        AND txns.stated_key_id = stated_keys.id)
ORDER BY stated_keys.id;

-- name: ClearStatedKeyGroups :exec
UPDATE stated_keys SET group_id = NULL
WHERE user_id = $1 AND group_id IS NOT NULL;

-- name: SetStatedKeyGroups :exec
-- Skips any key that a resolution associated after the caller listed it.
UPDATE stated_keys SET group_id = v.group_id
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@group_ids::uuid[]) AS group_id) AS v
WHERE stated_keys.user_id = @user_id::uuid AND stated_keys.id = v.id
  AND stated_keys.instr_id IS NULL;
