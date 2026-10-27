-- name: LockCorpActAnswer :exec
-- Serialises the writes of one datasource's answer for one ticker, for the
-- transaction. The seed keeps the key apart from the other advisory locks.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 2));

-- name: GetCorpActAnswer :one
SELECT corp_act_answers.id, fetch_keys.answered_at::timestamptz AS answered_at
FROM corp_act_answers
JOIN fetch_keys ON fetch_keys.id = corp_act_answers.fetch_key_id
WHERE corp_act_answers.datasource = @datasource
  AND corp_act_answers.market = @market
  AND corp_act_answers.value = @value;

-- name: CreateCorpActAnswer :exec
INSERT INTO corp_act_answers (id, fetch_key_id, datasource, market, value, covered_from, covered_before)
VALUES (@id, @fetch_key_id, @datasource, @market, @value, sqlc.narg(covered_from), sqlc.narg(covered_before));

-- name: UpdateCorpActAnswer :exec
UPDATE corp_act_answers
SET fetch_key_id = @fetch_key_id, covered_from = sqlc.narg(covered_from), covered_before = sqlc.narg(covered_before)
WHERE id = @id;

-- name: ListAnswerCorpActs :many
SELECT * FROM corp_acts WHERE answer_id = @answer_id ORDER BY ex_date, id;

-- name: CreateCorpAct :exec
INSERT INTO corp_acts (id, answer_id, ex_date, ratio_from, ratio_to, type, handled)
VALUES (@id, @answer_id, @ex_date, @ratio_from, @ratio_to, sqlc.narg(type), @handled);

-- name: DeleteCorpActs :exec
DELETE FROM corp_acts WHERE id = ANY(@ids::uuid[]);

-- name: ListDisagreedCorpActs :many
-- The corporate actions among ids that a disagreement finding already names.
SELECT DISTINCT corp_act_id::uuid FROM findings
WHERE kind = 'corp_act_disagreement' AND corp_act_id = ANY(@ids::uuid[]);

-- name: ListCorpActAnswers :many
-- The answers for the tickers whose market is in markets and whose value is
-- in values. The caller keeps the pairs it asked for. Each answer comes with
-- its datasource's precedence.
SELECT sqlc.embed(corp_act_answers), datasources.precedence
FROM corp_act_answers
JOIN datasources ON datasources.name = corp_act_answers.datasource
WHERE corp_act_answers.market = ANY(@markets::text[])
  AND corp_act_answers.value = ANY(@values::text[])
ORDER BY datasources.precedence, corp_act_answers.market, corp_act_answers.value;

-- name: ListCorpActs :many
-- The actions of the answers that ListCorpActAnswers reads, with an ex-date
-- in the period from up to before.
SELECT corp_acts.*
FROM corp_acts
JOIN corp_act_answers ON corp_act_answers.id = corp_acts.answer_id
WHERE corp_act_answers.market = ANY(@markets::text[])
  AND corp_act_answers.value = ANY(@values::text[])
  AND corp_acts.ex_date >= @from_day AND corp_acts.ex_date < @before_day
ORDER BY corp_acts.ex_date, corp_acts.id;

-- name: ListStatedSplits :many
-- The stated splits of the keys that name one of instrs or are dated
-- through a ticker in values. A user_id limits them to that user, and NULL
-- reads every user's. Values carry no market, so the caller keeps only the
-- keys dated through a ticker at a market it asked for.
SELECT sqlc.embed(stmt_splits), sqlc.embed(stated_keys)
FROM stmt_splits
JOIN stated_keys ON stated_keys.id = stmt_splits.stated_key_id
WHERE (stated_keys.instr_id = ANY(@instrs::uuid[]) OR stated_keys.via_value = ANY(@values::text[]))
  AND (sqlc.narg(user_id)::uuid IS NULL OR stmt_splits.user_id = sqlc.narg(user_id))
ORDER BY stmt_splits.stated_key_id, stmt_splits.effective_date, stmt_splits.stmt_id, stmt_splits.ordinal;

-- name: ListTickersNaming :many
-- The tickers, with their markets, that an assertion pairs with one of
-- instruments.
SELECT DISTINCT instr_id::uuid AS instr_id, domain AS market, value
FROM identifier_assertions
WHERE type = 'mic_ticker' AND instr_id = ANY(@instrs::uuid[])
ORDER BY instr_id, market, value;

-- name: ListUnmatchedSplits :many
-- The key and day of each unmatched split finding on one of keys.
SELECT DISTINCT stated_key_id::uuid AS stated_key_id, effective_date::date AS effective_date
FROM findings
WHERE kind = 'unmatched_split' AND stated_key_id = ANY(@keys::uuid[]);
