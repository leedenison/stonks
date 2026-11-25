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

-- name: ListRateListings :many
-- Each listing of a cash instrument in a family other than its own, with the
-- family code of the instrument. The price of such a listing is the rate of
-- the currency.
SELECT listings.id, listings.currency, currencies.code
FROM listings
JOIN instrs ON instrs.id = listings.instr_id AND instrs.asset_class = 'cash'
JOIN identifiers ON identifiers.instr_id = instrs.id AND identifiers.type = 'currency'
JOIN currencies ON currencies.code = identifiers.value AND currencies.family = currencies.code
WHERE listings.currency <> currencies.code
ORDER BY currencies.code, listings.currency;

-- name: CreateRateListings :exec
-- Creates a USD listing for the cash instrument of each currency in codes
-- that lacks one. Rows go in order of code, so two callers never wait on each
-- other in a cycle.
INSERT INTO listings (id, instr_id, currency)
SELECT k.id, identifiers.instr_id, 'USD'
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@codes::text[]) AS code) AS k
JOIN identifiers ON identifiers.type = 'currency' AND identifiers.value = k.code
JOIN instrs ON instrs.id = identifiers.instr_id AND instrs.asset_class = 'cash'
ORDER BY k.code
ON CONFLICT (instr_id, currency) DO NOTHING;

-- name: ListPrices :many
-- Returns the price of each listing on each day from from_day up to, but not
-- including, before. A day that several datasources priced takes the price of
-- the one that ranks first. A disabled datasource keeps its rank.
SELECT DISTINCT ON (price_coverage.listing_id, prices.day)
       price_coverage.listing_id, prices.day, prices.price, prices.as_at, price_coverage.datasource
FROM prices
JOIN price_coverage ON price_coverage.id = prices.coverage_id
JOIN datasources ON datasources.name = price_coverage.datasource
WHERE price_coverage.listing_id = ANY(@listing_ids::uuid[])
  AND prices.day >= @from_day AND prices.day < @before
ORDER BY price_coverage.listing_id, prices.day, datasources.precedence;

-- name: ListPricesBefore :many
-- Returns the latest price of each listing before the day given. The price
-- comes from the datasource that ranks first on the day it prices.
SELECT DISTINCT ON (price_coverage.listing_id)
       price_coverage.listing_id, prices.day, prices.price, prices.as_at, price_coverage.datasource
FROM prices
JOIN price_coverage ON price_coverage.id = prices.coverage_id
JOIN datasources ON datasources.name = price_coverage.datasource
WHERE price_coverage.listing_id = ANY(@listing_ids::uuid[]) AND prices.day < @day
ORDER BY price_coverage.listing_id, prices.day DESC, datasources.precedence;
