-- name: CreatePriceCoverage :exec
INSERT INTO price_coverage (id, listing_id, datasource, fetch_key_id, covered_from, covered_before)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreatePrices :copyfrom
INSERT INTO prices (coverage_id, day, price, as_at)
VALUES ($1, $2, $3, $4);

-- name: ListPriceCoverage :many
SELECT * FROM price_coverage
WHERE listing_id = ANY(@listing_ids::uuid[])
ORDER BY listing_id, datasource, covered_from;

-- name: MovePriceCoverage :exec
UPDATE price_coverage SET listing_id = @listing_id WHERE id = @id;

-- name: MovePrices :exec
-- Reassigns the prices of the coverage row from to the coverage row to, for
-- the days from day up to before.
UPDATE prices SET coverage_id = sqlc.arg('to')
WHERE coverage_id = sqlc.arg('from') AND day >= @day AND day < @before;
