-- +goose Up

-- A replay is a run of kind 'replay' and this row. source_id names a run of
-- kind 'statement' or 'resolution' of the same user.
CREATE TABLE replays (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_id  uuid NOT NULL,
    -- datasource is the scope: the keys the datasource serves and has not
    -- yet answered. NULL selects the keys left unavailable.
    datasource text REFERENCES datasources (name),
    started_by uuid NOT NULL REFERENCES users (id),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (source_id, user_id) REFERENCES runs (id, user_id) ON DELETE CASCADE
);

-- The latest resolution of a key is its row with the greatest run id.
CREATE INDEX resolution_keys_stated_key_idx ON resolution_keys (stated_key_id, run_id DESC);
