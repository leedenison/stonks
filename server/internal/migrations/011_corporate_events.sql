-- +goose Up

-- A corporate event answer is the latest answer of one datasource for one
-- ticker at its market. Its fetch key states the moment of the answer, and a
-- later answer replaces it. An empty answer is a row too.
CREATE TABLE corporate_event_answers (
    id             uuid PRIMARY KEY,
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    datasource     text NOT NULL REFERENCES datasources (name),
    market         text NOT NULL,
    value          text NOT NULL,
    -- covered_from and covered_before bound the days over which the datasource
    -- reported every corporate event of the ticker, ending on the day it
    -- answered. Both are NULL where it reported no coverage.
    covered_from   date,
    covered_before date,
    CHECK (market <> ''),
    CHECK ((covered_from IS NULL) = (covered_before IS NULL)),
    CHECK (covered_from < covered_before),
    UNIQUE (datasource, market, value)
);

CREATE INDEX corporate_event_answers_value_idx ON corporate_event_answers (market, value);
CREATE INDEX corporate_event_answers_fetch_key_idx ON corporate_event_answers (fetch_key_id);

-- A corporate event is an event that a datasource reported for a ticker with a
-- ratio. It may restate the quantity held, as a split, a reverse split or a
-- stock dividend does, or be of a type the system does not handle, such as an
-- exchange for another instrument. It applies to whatever the ticker named on
-- ex_date, which validity decides on read. Several rows may share a ticker and
-- a day.
CREATE TABLE corporate_events (
    id         uuid    PRIMARY KEY,
    answer_id  uuid    NOT NULL REFERENCES corporate_event_answers (id) ON DELETE CASCADE,
    ex_date    date    NOT NULL,
    -- ratio_from units before the event become ratio_to units after it.
    ratio_from numeric NOT NULL,
    ratio_to   numeric NOT NULL,
    -- type is the datasource's own word for the event, NULL where it states
    -- none.
    type       text,
    -- handled is whether the system restates a quantity by the event.
    handled    boolean NOT NULL,
    CHECK (ratio_from > 0),
    CHECK (ratio_to > 0)
);

CREATE INDEX corporate_events_answer_idx ON corporate_events (answer_id, ex_date);

ALTER TABLE findings ADD COLUMN corporate_event_id uuid REFERENCES corporate_events (id) ON DELETE CASCADE;
ALTER TABLE findings ADD CHECK
    ((kind IN ('unhandled_corporate_event', 'corporate_event_disagreement')) = (corporate_event_id IS NOT NULL));

CREATE INDEX findings_corporate_event_idx ON findings (corporate_event_id);
