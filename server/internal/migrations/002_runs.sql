-- +goose Up

CREATE TYPE run_kind AS ENUM ('statement', 'resolution', 'fetch');
CREATE TYPE run_trigger AS ENUM ('user', 'administrator', 'run');
CREATE TYPE run_state AS ENUM ('pending', 'running', 'completed', 'failed', 'interrupted');

-- A run is one unit of background work: a statement, ingesting one batch of
-- transactions a user submitted, a resolution answering the stated keys of a
-- statement, or a fetch asking one datasource for one kind of data. The row
-- exists before the work starts and is what its outcome is read against.
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
