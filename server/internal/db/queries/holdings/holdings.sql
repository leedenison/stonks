-- name: ListInstrumentHoldings :many
-- Cash is summed in its family's unit, so pence count as hundredths of a
-- pound. A security's quantity stays in shares whatever its currency.
SELECT stated_keys.instrument_id::uuid AS instrument_id, instruments.asset_class,
       SUM(transactions.quantity * CASE WHEN instruments.asset_class = 'cash' THEN COALESCE(currencies.scale, 1) ELSE 1 END)::numeric AS quantity
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
JOIN instruments ON instruments.id = stated_keys.instrument_id
LEFT JOIN currencies ON currencies.code = stated_keys.currency
WHERE transactions.user_id = @user_id::uuid
GROUP BY stated_keys.instrument_id, instruments.asset_class
HAVING SUM(transactions.quantity * CASE WHEN instruments.asset_class = 'cash' THEN COALESCE(currencies.scale, 1) ELSE 1 END) <> 0
ORDER BY stated_keys.instrument_id;

-- name: ListGroupHoldings :many
SELECT stated_keys.group_id::uuid AS group_id,
       SUM(transactions.quantity)::numeric AS quantity
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
WHERE transactions.user_id = @user_id::uuid
  AND stated_keys.group_id IS NOT NULL
GROUP BY stated_keys.group_id
HAVING SUM(transactions.quantity) <> 0
ORDER BY stated_keys.group_id;

-- name: ListHoldingKeys :many
-- The user's keys that have a transaction, each with the sum of its
-- transactions, earliest key first.
SELECT sqlc.embed(stated_keys), SUM(transactions.quantity)::numeric AS quantity
FROM stated_keys
JOIN transactions ON transactions.stated_key_id = stated_keys.id
WHERE stated_keys.user_id = @user_id::uuid
GROUP BY stated_keys.id
ORDER BY stated_keys.id;

-- name: ListListingNames :many
-- Each listing of the instruments, with the ticker that names it and its
-- venue. The venue is the common name, or the MIC where it has none. The
-- ticker is null and the venue empty where the listing has no ticker.
SELECT listings.id AS listing_id, listings.instrument_id, listings.currency,
       listing_tickers.domain AS ticker_domain, listing_tickers.value AS ticker_value,
       COALESCE(venue_names.name, listing_tickers.domain, '')::text AS venue
FROM listings
LEFT JOIN listing_tickers ON listing_tickers.listing_id = listings.id AND listing_tickers.rank = 1
LEFT JOIN venue_names ON venue_names.mic = listing_tickers.domain
WHERE listings.instrument_id = ANY(@instrument_ids::uuid[])
ORDER BY listings.instrument_id, listings.currency;
