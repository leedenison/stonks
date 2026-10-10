-- name: ListInstrumentHoldings :many
SELECT stated_keys.instrument_id::uuid AS instrument_id, instruments.asset_class,
       SUM(transactions.quantity)::numeric AS quantity
FROM transactions
JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
JOIN instruments ON instruments.id = stated_keys.instrument_id
WHERE transactions.user_id = @user_id::uuid
GROUP BY stated_keys.instrument_id, instruments.asset_class
HAVING SUM(transactions.quantity) <> 0
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
-- transactions, newest statement first.
SELECT sqlc.embed(stated_keys), SUM(transactions.quantity)::numeric AS quantity
FROM stated_keys
JOIN transactions ON transactions.stated_key_id = stated_keys.id
WHERE stated_keys.user_id = @user_id::uuid
GROUP BY stated_keys.id
ORDER BY stated_keys.statement_id DESC, stated_keys.id;

-- name: ListListingNames :many
-- Each listing of the instruments with the ticker that names it, chosen by
-- the listing's primary venue, then by the rank in venue_names, then by
-- MIC, and the venue's common name, or its MIC where it has none. The
-- ticker is null and the venue empty where the listing has no ticker.
SELECT * FROM (
    SELECT DISTINCT ON (listings.id)
        listings.id AS listing_id, listings.instrument_id, listings.currency,
        identifiers.domain AS ticker_domain, identifiers.value AS ticker_value,
        COALESCE(venue_names.name, identifiers.domain, '') AS venue
    FROM listings
    LEFT JOIN identifiers ON identifiers.listing_id = listings.id AND identifiers.type = 'mic_ticker'
    LEFT JOIN venue_names ON venue_names.mic = identifiers.domain
    WHERE listings.instrument_id = ANY(@instrument_ids::uuid[])
    ORDER BY listings.id, (identifiers.domain = listings.primary_mic) DESC NULLS LAST,
        venue_names.rank NULLS LAST, identifiers.domain
) AS named
ORDER BY instrument_id, currency;

-- name: ListStatedKeysOfGroups :many
SELECT * FROM stated_keys
WHERE user_id = @user_id::uuid AND group_id = ANY(@group_ids::uuid[])
ORDER BY group_id, id;
