-- +goose Up

CREATE TYPE run_kind AS ENUM ('statement', 'resolution', 'fetch', 'replay', 'attestation',
    'extend_validity');
-- What started a run: 'user' and 'administrator' are a person's request, and
-- 'run' is a parent run starting a child.
CREATE TYPE run_trigger AS ENUM ('user', 'administrator', 'run');
CREATE TYPE run_state AS ENUM ('pending', 'running', 'completed', 'failed', 'interrupted');

-- A run is one unit of work: a statement, ingesting one batch of transactions a
-- user submitted, a resolution resolving stated keys, a replay re-resolving the
-- keys of one run, a fetch requesting one kind of data from one datasource, an
-- attestation recording an administrator's assertions, or an extend validity run
-- asking the datasources again about every held ticker. Most run in the
-- background. A resolution a user starts runs in the call that starts it. The
-- row exists before the work starts and carries its outcome.
--
-- A run acts for its user. A system run acts for the system and has no user.
CREATE TABLE runs (
    id          uuid        PRIMARY KEY,
    user_id     uuid        REFERENCES users (id) ON DELETE CASCADE,
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
    FOREIGN KEY (parent_id) REFERENCES runs (id) ON DELETE CASCADE
);

-- runs_parent_user refuses a child of another user's run. A system run may be
-- the parent or the child of any run.
-- +goose StatementBegin
CREATE FUNCTION runs_parent_user() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    parent_user uuid;
BEGIN
    IF NEW.parent_id IS NULL OR NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT user_id INTO parent_user FROM runs WHERE id = NEW.parent_id;
    IF parent_user IS NOT NULL AND parent_user <> NEW.user_id THEN
        RAISE EXCEPTION 'run % belongs to another user than its parent %', NEW.id, NEW.parent_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER runs_parent_user BEFORE INSERT OR UPDATE OF parent_id, user_id ON runs
    FOR EACH ROW EXECUTE FUNCTION runs_parent_user();

-- runs_parent_idx serves the walk from a run to its children, and with a NULL
-- parent_id the newest top-level runs.
CREATE INDEX runs_parent_idx ON runs (parent_id, id);
CREATE INDEX runs_user_idx ON runs (user_id, id);
