-- name: ListDatedTxns :many
-- The transactions of the user's dated keys, by key then as-at date. The
-- key's ticker, currency and group are on the key, which ListHoldingKeys
-- carries.
SELECT txns.stated_key_id, txns.as_at, txns.quantity
FROM txns
JOIN stated_keys ON stated_keys.id = txns.stated_key_id
WHERE txns.user_id = @user_id::uuid AND stated_keys.via_ticker_id IS NOT NULL
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
-- Each listing of the instruments with every ticker that reaches it, ranked
-- as the listing's name, and the common name of the ticker's venue. A listing
-- without a ticker comes once, with a null ticker.
SELECT listings.id AS listing_id, listings.instr_id, listings.currency,
       listing_tickers.ticker_id, listing_tickers.market, listing_tickers.value AS ticker_value,
       listing_tickers.venue, listing_tickers.relation, listing_tickers.rank,
       venue_names.name AS venue_name
FROM listings
LEFT JOIN listing_tickers ON listing_tickers.listing_id = listings.id
LEFT JOIN venue_names ON venue_names.mic = listing_tickers.venue
WHERE listings.instr_id = ANY(@instr_ids::uuid[])
ORDER BY listings.instr_id, listings.currency, listing_tickers.rank;
