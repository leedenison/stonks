-- +goose Up

CREATE TYPE run_kind AS ENUM ('statement', 'resolution');
CREATE TYPE run_trigger AS ENUM ('user', 'run');
CREATE TYPE run_state AS ENUM ('pending', 'running', 'completed', 'failed', 'interrupted');

-- A run is one unit of background work: a statement, ingesting one batch of
-- transactions a user submitted, or a resolution answering the stated keys
-- of a statement. The row exists before the work starts and is what its
-- outcome is read against.
--
-- The trigger is what started the run. A run of trigger 'run' was started by
-- another run and names it as parent; a run of trigger 'user' has none.
--
-- The state is 'pending' until the work starts, 'running' once it has, and
-- 'completed', 'failed' or 'interrupted' once it stops. Entering 'running'
-- sets started_at and entering any of the last three sets finished_at. error
-- is set only for a 'failed' run. An 'interrupted' run is one the process
-- stopped before the work ended; nothing recorded why.
CREATE TABLE runs (
    id          uuid        PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users (id),
    kind        run_kind    NOT NULL,
    trigger     run_trigger NOT NULL,
    parent_id   uuid        REFERENCES runs (id),
    state       run_state   NOT NULL DEFAULT 'pending',
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,
    CHECK ((trigger = 'run') = (parent_id IS NOT NULL)),
    UNIQUE (id, user_id)
);
