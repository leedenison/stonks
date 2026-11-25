-- name: CreatePriceCoverage :exec
INSERT INTO price_coverage (id, listing_id, datasource, fetch_key_id, covered_from, covered_before)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreatePrices :copyfrom
INSERT INTO prices (coverage_id, day, price, as_at)
VALUES ($1, $2, $3, $4);
