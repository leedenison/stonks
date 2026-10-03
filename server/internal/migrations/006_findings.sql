-- +goose Up

-- The taxonomy of findings:
-- 'block' means the run wrote a datasource block.
-- 'dropped' means the resolution dropped a candidate group a datasource
-- offered because it contradicted the stated data, a higher precedence
-- response or the winner. When a group is outranked within its datasource,
-- or does not name the identifier sent, the drop is routine and is recorded
-- on the resolution key rather than as a finding.
-- 'contradiction' means a response contradicted the instrument the key
-- already matched, and the database's precedence decided.
-- 'merged' means the response merged two instruments.
CREATE TYPE finding_kind AS ENUM ('block', 'dropped', 'contradiction', 'merged');

-- The step of the choice that dropped a candidate group: it contradicted the
-- stated data, was inconsistent with a higher precedence response, or shared
-- no stable identifier with the winner.
CREATE TYPE drop_step AS ENUM ('stated', 'precedence', 'corroboration');

-- A finding highlights abnormal run outcomes to administrators.
CREATE TABLE findings (
    id            uuid         PRIMARY KEY,
    run_id        uuid         NOT NULL REFERENCES runs (id),
    kind          finding_kind NOT NULL,
    block_id      uuid         UNIQUE REFERENCES datasource_blocks (id),
    -- stated_key_id is the finding's key, for every kind but block.
    stated_key_id uuid         REFERENCES stated_keys (id),
    -- fetch_key_id is the response involved, where one was.
    fetch_key_id  uuid         REFERENCES fetch_keys (id),
    step          drop_step,
    -- detail says the grounds in words.
    detail        text,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    cleared_at    timestamptz,
    CHECK ((kind = 'block') = (block_id IS NOT NULL)),
    CHECK ((kind = 'block') = (stated_key_id IS NULL)),
    CHECK ((kind = 'block') = (detail IS NULL)),
    CHECK (fetch_key_id IS NULL OR stated_key_id IS NOT NULL),
    CHECK ((kind = 'dropped') = (step IS NOT NULL)),
    CHECK (kind <> 'dropped' OR fetch_key_id IS NOT NULL)
);

CREATE INDEX findings_run_idx ON findings (run_id);
CREATE INDEX findings_stated_key_idx ON findings (stated_key_id);
CREATE INDEX findings_open_idx ON findings (created_at) WHERE cleared_at IS NULL;
