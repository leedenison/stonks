-- name: LockAdvisory :exec
-- Takes the transaction's advisory lock on each name. lock.go builds every
-- name and passes them sorted, so two transactions take the locks in one
-- order.
SELECT pg_advisory_xact_lock(hashtextextended(n, 0)) FROM unnest(@names::text[]) AS n;
