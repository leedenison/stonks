-- name: ListHoldings :many
SELECT transactions.instrument_id, instruments.asset_class,
       SUM(transactions.quantity)::numeric AS quantity
FROM transactions
JOIN instruments ON instruments.id = transactions.instrument_id
WHERE transactions.user_id = @user_id::uuid
GROUP BY transactions.instrument_id, instruments.asset_class
HAVING SUM(transactions.quantity) <> 0
ORDER BY transactions.instrument_id;

-- name: ListHeldIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id IN (
    SELECT instrument_id FROM transactions
    WHERE user_id = @user_id::uuid
    GROUP BY instrument_id
    HAVING SUM(quantity) <> 0)
  AND (owner_id IS NULL OR owner_id = @user_id::uuid)
ORDER BY instrument_id, type, domain, value;
