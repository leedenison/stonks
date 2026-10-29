-- +goose Up

-- The taxonomy of findings:
-- 'block' means the run wrote a datasource block.
-- 'dropped' means the resolution dropped a set of identifiers a datasource
-- offered because it contradicted the stated data, a higher precedence
-- response or the winner. When a set is outranked within its datasource,
-- or does not name the identifier sent, the drop is routine and is recorded
-- on the resolution key rather than as a finding.
-- 'contradiction' means that two sources name different instruments for
-- one key: the identifiers the key states, the user's confirmed keys, or a
-- response. The database's precedence or the datasources decided between
-- them.
-- 'merged' means the response merged two instruments.
-- 'identifier_disagreement' means two datasources asserted different
-- identifier facts for one ticker over a run of days. Precedence kept one.
-- Unlike a contradiction, it concerns a ticker rather than a key. The run
-- whose write completed the pair records it, and the finding names the losing
-- assertion's fetch key. A later answer of one datasource replaces its earlier
-- answer without raising a finding.
-- 'broken_ticker_validity_bracket' means an edge cut between two assertions
-- that name one instrument, and no identifier attestation overruled the
-- edge. The run that wrote the edge records it, and the finding names the
-- edge's fetch key.
-- 'missed_event' means two assertions imply a change on one of a run of
-- days, and the assertion coverage of the market's identifier event source
-- holds every one of those days. That datasource did not report an edge
-- there, so the assertion coverage wins. The run that wrote the assertion
-- coverage or either assertion records it, and the finding names the fetch
-- key of the assertion coverage.
-- 'unhandled_corp_act' means a datasource reported a corporate action of
-- a type the system does not restate a quantity by. The fetch that wrote the
-- corporate action records it, and the finding names the corporate action.
-- 'unmatched_split' means a statement stated a split that the calendar lacks:
-- no corporate action of the split's instrument falls within 7 days either
-- side, with an equal ratio where the statement states one. A corporate action
-- fetch that covers the instrument on the split's date records it. The
-- finding names the stated key and the split's effective date. One finding
-- stands for each key and day, so clearing the finding dismisses that split permanently.
CREATE TYPE finding_kind AS ENUM ('block', 'dropped', 'contradiction', 'merged',
    'identifier_disagreement', 'broken_ticker_validity_bracket', 'missed_event',
    'unhandled_corp_act', 'unmatched_split');

-- The step of the choice that dropped a set of identifiers: it contradicted
-- the stated data, was inconsistent with a higher precedence response, or
-- shared neither a stable identifier nor a same-day ticker in one market
-- with the winner.
CREATE TYPE drop_step AS ENUM ('stated', 'precedence', 'corroboration');

-- A finding highlights abnormal run outcomes to administrators.
-- 011_corp_acts.sql adds corp_act_id, the corporate action a
-- finding names.
CREATE TABLE findings (
    id            uuid         PRIMARY KEY,
    run_id        uuid         NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    kind          finding_kind NOT NULL,
    block_id      uuid         UNIQUE REFERENCES datasource_blocks (id) ON DELETE CASCADE,
    -- stated_key_id is the finding's key, for the kinds that concern a key.
    stated_key_id uuid         REFERENCES stated_keys (id) ON DELETE CASCADE,
    -- effective_date is the day of the stated split that an unmatched split
    -- finding concerns.
    effective_date date,
    -- fetch_key_id is the response involved, where one was.
    fetch_key_id  uuid         REFERENCES fetch_keys (id) ON DELETE CASCADE,
    step          drop_step,
    -- detail says the grounds in words.
    detail        text,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    cleared_at    timestamptz,
    CHECK ((kind = 'block') = (block_id IS NOT NULL)),
    CHECK ((kind IN ('dropped', 'contradiction', 'merged', 'unmatched_split')) = (stated_key_id IS NOT NULL)),
    CHECK ((kind = 'unmatched_split') = (effective_date IS NOT NULL)),
    CHECK ((kind = 'block') = (detail IS NULL)),
    CHECK ((kind = 'dropped') = (step IS NOT NULL)),
    CHECK (kind NOT IN ('dropped', 'identifier_disagreement', 'broken_ticker_validity_bracket', 'missed_event')
           OR fetch_key_id IS NOT NULL)
);

CREATE INDEX findings_run_idx ON findings (run_id);
CREATE INDEX findings_stated_key_idx ON findings (stated_key_id);
