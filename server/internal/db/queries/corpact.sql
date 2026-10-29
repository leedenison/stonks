-- name: LockCorpActAnswer :exec
-- Serialises the writes of one datasource's answer for one ticker, for the
-- transaction. The seed keeps the key apart from the other advisory locks.
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 2));

-- name: GetCorpActAnswer :one
SELECT corp_act_answers.id, fetch_keys.answered_at::timestamptz AS answered_at
FROM corp_act_answers
JOIN fetch_keys ON fetch_keys.id = corp_act_answers.fetch_key_id
WHERE corp_act_answers.datasource = @datasource
  AND corp_act_answers.ticker_id = @ticker_id;

-- name: CreateCorpActAnswer :exec
INSERT INTO corp_act_answers (id, fetch_key_id, datasource, ticker_id, covered_from, covered_before)
VALUES (@id, @fetch_key_id, @datasource, @ticker_id, sqlc.narg(covered_from), sqlc.narg(covered_before));

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

-- name: ListCorpActSources :many
-- Each market that names a corporate actions source, with that datasource.
SELECT code, corp_act_source::text AS datasource FROM markets
WHERE corp_act_source IS NOT NULL
ORDER BY code;

-- name: ListCorpActAnswers :many
-- The answers of each market's corporate actions source for the tickers whose
-- market and value are a pair the arrays hold at one index. Each answer comes
-- with its ticker's spelling.
SELECT sqlc.embed(corp_act_answers), tickers.market, tickers.value
FROM (SELECT unnest(@markets::text[]) AS market, unnest(@values::text[]) AS value) AS asked
JOIN tickers ON tickers.market = asked.market AND tickers.value = asked.value
JOIN markets ON markets.code = tickers.market
JOIN corp_act_answers ON corp_act_answers.ticker_id = tickers.id
    AND corp_act_answers.datasource = markets.corp_act_source
ORDER BY tickers.market, tickers.value;

-- name: ListCorpActs :many
-- The actions of the answers that ListCorpActAnswers reads, with an ex-date
-- in the period from up to before.
SELECT corp_acts.*
FROM (SELECT unnest(@markets::text[]) AS market, unnest(@values::text[]) AS value) AS asked
JOIN tickers ON tickers.market = asked.market AND tickers.value = asked.value
JOIN markets ON markets.code = tickers.market
JOIN corp_act_answers ON corp_act_answers.ticker_id = tickers.id
    AND corp_act_answers.datasource = markets.corp_act_source
JOIN corp_acts ON corp_acts.answer_id = corp_act_answers.id
WHERE corp_acts.ex_date >= @from_day AND corp_acts.ex_date < @before_day
ORDER BY corp_acts.ex_date, corp_acts.id;

-- name: ListStatedSplits :many
-- The stated splits whose key names one of instrs on the split's effective
-- date, each with that instrument. A key dated through one of tickers names
-- what its ticker's validity names on the day. A user_id limits them to that user, and NULL reads every user's.
SELECT sqlc.embed(stmt_splits),
       COALESCE(stated_keys.instr_id, ticker_validity.instr_id)::uuid AS instr_id
FROM stmt_splits
JOIN stated_keys ON stated_keys.id = stmt_splits.stated_key_id
LEFT JOIN ticker_validity ON ticker_validity.ticker_id = stated_keys.via_ticker_id
    AND stated_keys.via_ticker_id = ANY(@tickers::uuid[])
    AND (ticker_validity.valid_from IS NULL OR ticker_validity.valid_from <= stmt_splits.effective_date)
    AND (ticker_validity.valid_before IS NULL OR stmt_splits.effective_date < ticker_validity.valid_before)
WHERE COALESCE(stated_keys.instr_id, ticker_validity.instr_id) = ANY(@instrs::uuid[])
  AND (sqlc.narg(user_id)::uuid IS NULL OR stmt_splits.user_id = sqlc.narg(user_id))
ORDER BY stmt_splits.stated_key_id, stmt_splits.effective_date, stmt_splits.stmt_id, stmt_splits.ordinal;

-- name: ListInstrMarkets :many
-- The market of the primary venues of each of instrs. It leaves out an
-- instrument whose listings state no primary venue. It also leaves out one
-- whose listings state primary venues in two markets.
SELECT listings.instr_id, MIN(market_members.market)::text AS market
FROM listings
JOIN market_members ON market_members.operating_mic = listings.primary_mic
WHERE listings.instr_id = ANY(@instrs::uuid[])
GROUP BY listings.instr_id
HAVING COUNT(DISTINCT market_members.market) = 1;

-- name: ListTickersNaming :many
-- The tickers whose ticker validity names one of instrs on some day.
SELECT DISTINCT ticker_validity.instr_id, tickers.id AS ticker_id, tickers.market, tickers.value
FROM ticker_validity
JOIN tickers ON tickers.id = ticker_validity.ticker_id
WHERE ticker_validity.instr_id = ANY(@instrs::uuid[])
ORDER BY ticker_validity.instr_id, tickers.market, tickers.value;
