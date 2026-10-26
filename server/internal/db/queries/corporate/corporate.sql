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
