-- name: LockCorporateAnswer :exec
-- Serialises the writes of one datasource's answer for one ticker, for the
-- transaction. The seed keeps the key apart from the other advisory locks.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 2));

-- name: GetCorporateAnswer :one
SELECT corporate_event_answers.id, fetch_keys.answered_at::timestamptz AS answered_at
FROM corporate_event_answers
JOIN fetch_keys ON fetch_keys.id = corporate_event_answers.fetch_key_id
WHERE corporate_event_answers.datasource = @datasource
  AND corporate_event_answers.market = @market
  AND corporate_event_answers.value = @value;

-- name: CreateCorporateAnswer :exec
INSERT INTO corporate_event_answers (id, fetch_key_id, datasource, market, value, covered_from, covered_before)
VALUES (@id, @fetch_key_id, @datasource, @market, @value, sqlc.narg(covered_from), sqlc.narg(covered_before));

-- name: UpdateCorporateAnswer :exec
UPDATE corporate_event_answers
SET fetch_key_id = @fetch_key_id, covered_from = sqlc.narg(covered_from), covered_before = sqlc.narg(covered_before)
WHERE id = @id;

-- name: ListAnswerEvents :many
SELECT * FROM corporate_events WHERE answer_id = @answer_id ORDER BY ex_date, id;

-- name: CreateCorporateEvent :exec
INSERT INTO corporate_events (id, answer_id, ex_date, ratio_from, ratio_to, type, handled)
VALUES (@id, @answer_id, @ex_date, @ratio_from, @ratio_to, sqlc.narg(type), @handled);

-- name: DeleteCorporateEvents :exec
DELETE FROM corporate_events WHERE id = ANY(@ids::uuid[]);

-- name: ListDisagreedEvents :many
-- The corporate events among ids that a disagreement finding already names.
SELECT DISTINCT corporate_event_id::uuid FROM findings
WHERE kind = 'corporate_event_disagreement' AND corporate_event_id = ANY(@ids::uuid[]);

-- name: ListCorporateAnswers :many
-- The answers for the tickers whose market is in markets and whose value is
-- in values. The caller keeps the pairs it asked for. Each answer comes with
-- its datasource's precedence.
SELECT sqlc.embed(corporate_event_answers), datasources.precedence
FROM corporate_event_answers
JOIN datasources ON datasources.name = corporate_event_answers.datasource
WHERE corporate_event_answers.market = ANY(@markets::text[])
  AND corporate_event_answers.value = ANY(@values::text[])
ORDER BY datasources.precedence, corporate_event_answers.market, corporate_event_answers.value;

-- name: ListCorporateEvents :many
-- The events of the answers that ListCorporateAnswers reads, with an ex-date
-- in the period from up to before.
SELECT corporate_events.*
FROM corporate_events
JOIN corporate_event_answers ON corporate_event_answers.id = corporate_events.answer_id
WHERE corporate_event_answers.market = ANY(@markets::text[])
  AND corporate_event_answers.value = ANY(@values::text[])
  AND corporate_events.ex_date >= @from_day AND corporate_events.ex_date < @before_day
ORDER BY corporate_events.ex_date, corporate_events.id;

-- name: ListStatedSplits :many
-- The stated splits of the keys that name one of instruments or are dated
-- through a ticker in values. A user_id limits them to that user, and NULL
-- reads every user's. Values carry no market, so the caller keeps only the
-- keys dated through a ticker at a market it asked for.
SELECT sqlc.embed(statement_splits), sqlc.embed(stated_keys)
FROM statement_splits
JOIN stated_keys ON stated_keys.id = statement_splits.stated_key_id
WHERE (stated_keys.instrument_id = ANY(@instruments::uuid[]) OR stated_keys.via_value = ANY(@values::text[]))
  AND (sqlc.narg(user_id)::uuid IS NULL OR statement_splits.user_id = sqlc.narg(user_id))
ORDER BY statement_splits.stated_key_id, statement_splits.effective_date, statement_splits.statement_id, statement_splits.ordinal;

-- name: ListTickersNaming :many
-- The tickers, with their markets, that an assertion pairs with one of
-- instruments.
SELECT DISTINCT instrument_id::uuid AS instrument_id, domain AS market, value
FROM identifier_assertions
WHERE type = 'mic_ticker' AND instrument_id = ANY(@instruments::uuid[])
ORDER BY instrument_id, market, value;

-- name: ListUnmatchedSplits :many
-- The key and day of each unmatched split finding on one of keys.
SELECT DISTINCT stated_key_id::uuid AS stated_key_id, effective_date::date AS effective_date
FROM findings
WHERE kind = 'unmatched_split' AND stated_key_id = ANY(@keys::uuid[]);
