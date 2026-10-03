-- +goose Up

CREATE TYPE run_kind AS ENUM ('statement', 'resolution', 'fetch', 'replay');
-- What started a run: 'user' and 'administrator' are a person's request, and
-- 'run' is a parent run starting a child.
CREATE TYPE run_trigger AS ENUM ('user', 'administrator', 'run');
CREATE TYPE run_state AS ENUM ('pending', 'running', 'completed', 'failed', 'interrupted');

-- A run is one unit of background work: a statement, ingesting one batch of
-- transactions a user submitted, a resolution resolving stated keys, a replay
-- re-resolving the keys of one run, or a fetch requesting one kind of data
-- from one datasource. The row exists before the work starts and carries its
-- outcome.
CREATE TABLE runs (
    id          uuid        PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users (id),
    kind        run_kind    NOT NULL,
    -- trigger is what started the run.
    trigger     run_trigger NOT NULL,
    -- parent_id names the run that started this one.
    parent_id   uuid,
    -- state is 'pending' until the work starts, 'running' once it has, and
    -- 'completed', 'failed' or 'interrupted' once it stops. An 'interrupted'
    -- run is one the process stopped before the work ended.
    state       run_state   NOT NULL DEFAULT 'pending',
    -- error is set only for a 'failed' run.
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- started_at is set on entering 'running'.
    started_at  timestamptz,
    -- finished_at is set on entering a terminal state.
    finished_at timestamptz,
    CHECK ((trigger = 'run') = (parent_id IS NOT NULL)),
    UNIQUE (id, user_id),
    FOREIGN KEY (parent_id, user_id) REFERENCES runs (id, user_id)
);
