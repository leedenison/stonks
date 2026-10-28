-- name: CreateTickers :exec
-- Creates each ticker that has no row. The caller sorts the tickers, so two
-- creators of one set insert in one order.
INSERT INTO tickers (id, market, value)
SELECT k.id, k.market, k.value
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@markets::text[]) AS market,
             unnest(@values::text[]) AS value) AS k
ORDER BY k.market, k.value
ON CONFLICT (market, value) DO NOTHING;

-- name: ListTickers :many
-- The tickers whose market and value are a pair the arrays hold at one index.
SELECT tickers.*
FROM (SELECT unnest(@markets::text[]) AS market, unnest(@values::text[]) AS value) AS k
JOIN tickers ON tickers.market = k.market AND tickers.value = k.value
ORDER BY tickers.market, tickers.value;

-- name: ListTickersByID :many
SELECT * FROM tickers WHERE id = ANY(@ids::uuid[]) ORDER BY market, value;

-- name: LockTickers :many
-- Locks the rows of the tickers for the transaction, in id order. A fact
-- write takes these locks after its advisory identifier locks.
SELECT id FROM tickers WHERE id = ANY(@ids::uuid[]) ORDER BY id FOR UPDATE;

-- name: LockTickerBatch :many
-- Locks the next lim tickers after the id after, in id order. A NULL after
-- starts at the first.
SELECT id FROM tickers
WHERE sqlc.narg(after)::uuid IS NULL OR id > sqlc.narg(after)
ORDER BY id
LIMIT @lim::int
FOR UPDATE;
