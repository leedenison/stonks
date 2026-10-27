-- +goose Up

-- A corporate action answer is the latest answer of one datasource for one
-- ticker. Its fetch key states the moment of the answer, and a later answer
-- replaces it. An empty answer is a row too.
CREATE TABLE corp_act_answers (
    id             uuid PRIMARY KEY,
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    datasource     text NOT NULL REFERENCES datasources (name),
    ticker_id      uuid NOT NULL REFERENCES tickers (id),
    -- covered_from and covered_before bound the days over which the datasource
    -- reported every corporate action of the ticker, ending on the day it
    -- answered. Both are NULL where it reported no coverage.
    covered_from   date,
    covered_before date,
    CHECK ((covered_from IS NULL) = (covered_before IS NULL)),
    CHECK (covered_from < covered_before),
    UNIQUE (datasource, ticker_id)
);

CREATE INDEX corp_act_answers_ticker_idx ON corp_act_answers (ticker_id);
CREATE INDEX corp_act_answers_fetch_key_idx ON corp_act_answers (fetch_key_id);

-- A corporate action is a change to a company's shares that a datasource
-- reported for a ticker with a ratio. It may restate the quantity held, as a
-- split, a reverse split or a stock dividend does. It may instead be of a type
-- the system does not handle, such as an exchange for another instrument. A
-- provider may also state a price factor as a ratio, such as the fall in a
-- parent's price at a spin-off. It applies to whatever the ticker named on
-- ex_date, which ticker validity decides on read. Several rows may share a
-- ticker and a day.
CREATE TABLE corp_acts (
    id         uuid    PRIMARY KEY,
    answer_id  uuid    NOT NULL REFERENCES corp_act_answers (id) ON DELETE CASCADE,
    ex_date    date    NOT NULL,
    -- ratio_from units before the action become ratio_to units after it.
    ratio_from numeric NOT NULL,
    ratio_to   numeric NOT NULL,
    -- type is the datasource's own word for the action, NULL where it states
    -- none.
    type       text,
    -- handled is whether the system restates a quantity by the action. The
    -- datasource must handle its type. Its ratio must state a change in
    -- share count: both sides are whole, and in lowest terms the smaller is
    -- at most 100.
    handled    boolean NOT NULL,
    CHECK (ratio_from > 0),
    CHECK (ratio_to > 0)
);

CREATE INDEX corp_acts_answer_idx ON corp_acts (answer_id, ex_date);

ALTER TABLE findings ADD COLUMN corp_act_id uuid REFERENCES corp_acts (id) ON DELETE CASCADE;
ALTER TABLE findings ADD CHECK
    ((kind IN ('unhandled_corp_act', 'corp_act_disagreement')) = (corp_act_id IS NOT NULL));

CREATE INDEX findings_corp_act_idx ON findings (corp_act_id);
