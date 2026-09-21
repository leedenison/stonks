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

-- name: ListHeldIdentifiers :many
SELECT * FROM identifiers
WHERE instrument_id IN (
    SELECT stated_keys.instrument_id
    FROM transactions
    JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
    WHERE transactions.user_id = @user_id::uuid
      AND stated_keys.instrument_id IS NOT NULL
    GROUP BY stated_keys.instrument_id
    HAVING SUM(transactions.quantity) <> 0)
ORDER BY instrument_id, type, domain, value;

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

-- name: ListHeldGroupKeys :many
SELECT sqlc.embed(stated_keys), statements.broker
FROM stated_keys
JOIN statements ON statements.id = stated_keys.statement_id
WHERE stated_keys.user_id = @user_id::uuid
  AND stated_keys.group_id IN (
      SELECT stated_keys.group_id
      FROM transactions
      JOIN stated_keys ON stated_keys.id = transactions.stated_key_id
      WHERE transactions.user_id = @user_id::uuid
        AND stated_keys.group_id IS NOT NULL
      GROUP BY stated_keys.group_id
      HAVING SUM(transactions.quantity) <> 0)
ORDER BY stated_keys.group_id, stated_keys.id;
