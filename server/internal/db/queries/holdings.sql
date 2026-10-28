-- name: ListHoldingKeys :many
-- The user's keys that have a transaction, earliest key first. A key comes
-- once for each instrument its transactions name, with the sum of those
-- transactions. A NULL instrument sums the transactions that name nothing.
SELECT sqlc.embed(stated_keys), txn_instrs.instr_id, SUM(txn_instrs.quantity)::numeric AS quantity
FROM stated_keys
JOIN txn_instrs ON txn_instrs.stated_key_id = stated_keys.id
WHERE stated_keys.user_id = @user_id::uuid
GROUP BY stated_keys.id, txn_instrs.instr_id
ORDER BY stated_keys.id, txn_instrs.instr_id NULLS FIRST;

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
