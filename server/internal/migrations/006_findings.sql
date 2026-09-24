-- +goose Up

-- The taxonomy of findings:
-- 'block' means the run wrote a datasource block.
CREATE TYPE finding_kind AS ENUM ('block');

-- A finding highlights abnormal run outcomes to administrators.
CREATE TABLE findings (
    id         uuid         PRIMARY KEY,
    run_id     uuid         NOT NULL REFERENCES runs (id),
    kind       finding_kind NOT NULL,
    block_id   uuid         UNIQUE REFERENCES datasource_blocks (id),
    created_at timestamptz  NOT NULL DEFAULT now(),
    cleared_at timestamptz,
    CHECK ((kind = 'block') = (block_id IS NOT NULL))
);

CREATE INDEX findings_run_idx ON findings (run_id);
CREATE INDEX findings_open_idx ON findings (created_at) WHERE cleared_at IS NULL;
