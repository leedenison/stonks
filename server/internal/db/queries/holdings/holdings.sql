-- name: ListDatedTxns :many
-- The transactions of the user's dated keys, by key then as-at date. The
-- key's ticker, currency and group are on the key, which ListHoldingKeys
-- carries.
SELECT txns.stated_key_id, txns.as_at, txns.quantity
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
WHERE txns.user_id = @user_id::uuid AND stated_keys.via_type IS NOT NULL
ORDER BY txns.stated_key_id, txns.as_at, txns.id;

-- name: ListHoldingKeys :many
-- The user's keys that have a transaction, each with the sum of its
-- transactions, earliest key first.
SELECT sqlc.embed(stated_keys), SUM(txns.quantity)::numeric AS quantity
FROM stated_keys
JOIN txns ON txns.stated_key_id = stated_keys.id
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
