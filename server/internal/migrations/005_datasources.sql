-- +goose Up

-- A datasource is one external provider this instance may call.
CREATE TABLE datasources (
    -- name selects the integration. If the integration does not exist the
    -- service halts.
    name       text        PRIMARY KEY,
    enabled    boolean     NOT NULL DEFAULT true,
    -- precedence orders responses from multiple datasources.
    precedence integer     NOT NULL,
    credential text,
    endpoint   text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (name <> '')
);

-- The kind of data a fetch requests.
CREATE TYPE fetch_kind AS ENUM ('identity');

-- The result of a fetch:
-- 'served' means the datasource returned a, possibly empty, result set.
-- 'not_served' means the integration declared the key out of its scope.
-- 'blocked' means a fetch block suppressed the call.
-- 'failed_temporary' means the attempt failed with a temporary error.
-- 'failed_permanent' means the attempt failed with a permanent error.
CREATE TYPE fetch_outcome AS ENUM ('served', 'not_served', 'blocked',
    'failed_temporary', 'failed_permanent');

-- The scope of a block.
CREATE TYPE block_scope AS ENUM ('identifier', 'datasource');

-- A fetch represents requests for one set of keys to one datasource.
CREATE TABLE fetches (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id),
    datasource text        NOT NULL REFERENCES datasources (name),
    kind       fetch_kind  NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id),
    UNIQUE (user_id, id)
);

-- A fetch key is one fetched identifier within a fetch covering
-- several identifiers.
CREATE TABLE fetch_keys (
    id            uuid            PRIMARY KEY,
    fetch_id      uuid            NOT NULL,
    user_id       uuid            NOT NULL,
    stated_key_id uuid            NOT NULL REFERENCES stated_keys (id),
    outcome       fetch_outcome   NOT NULL,
    -- attempts is the number of calls made.
    attempts      smallint        NOT NULL,
    sent_type     identifier_type,
    sent_domain   text,
    sent_value    text,
    -- instrument_id is what the answer was attached to.
    instrument_id uuid            REFERENCES instruments (id),
    -- reason records the datasource's error when results are not returned.
    reason        text,
    created_at    timestamptz     NOT NULL DEFAULT now(),
    FOREIGN KEY (fetch_id, user_id) REFERENCES fetches (id, user_id),
    UNIQUE (fetch_id, stated_key_id),
    CHECK ((outcome = 'not_served') = (sent_type IS NULL)),
    CHECK ((sent_type IS NULL) = (sent_value IS NULL)),
    CHECK ((outcome = 'served') = (reason IS NULL)),
    CHECK ((outcome IN ('not_served', 'blocked')) = (attempts = 0)),
    CHECK (instrument_id IS NULL OR outcome = 'served')
);

CREATE INDEX fetch_keys_stated_key_idx ON fetch_keys (stated_key_id);
CREATE INDEX fetch_keys_instrument_idx ON fetch_keys (instrument_id);

-- The identifiers returned as part of a fetch result; an assertion by the
-- datasource that the identifiers refer to the same instrument at the time
-- of the fetch.
CREATE TABLE fetch_identifiers (
    fetch_key_id uuid            NOT NULL REFERENCES fetch_keys (id),
    type         identifier_type NOT NULL,
    domain       text,
    value        text            NOT NULL,
    created_at   timestamptz     NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (fetch_key_id, type, domain, value)
);

CREATE INDEX fetch_identifiers_value_idx ON fetch_identifiers (type, domain, value);

-- A block records that a call failed unrecoverably, and suppresses further calls
-- until an administrator clears it.
CREATE TABLE datasource_blocks (
    id           uuid        PRIMARY KEY,
    datasource   text        NOT NULL REFERENCES datasources (name),
    kind         fetch_kind  NOT NULL,
    -- scope is either a single identifier or an entire datasource.
    scope        block_scope NOT NULL,
    sent_type    identifier_type,
    sent_domain  text,
    sent_value   text,
    reason       text        NOT NULL,
    -- fetch_key_id is the call that created the block.
    fetch_key_id uuid        NOT NULL REFERENCES fetch_keys (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    cleared_at   timestamptz,
    CHECK ((scope = 'identifier') = (sent_type IS NOT NULL)),
    CHECK ((sent_type IS NULL) = (sent_value IS NULL))
);

CREATE UNIQUE INDEX datasource_blocks_identifier_idx
    ON datasource_blocks (datasource, kind, sent_type, sent_domain, sent_value)
    NULLS NOT DISTINCT
    WHERE cleared_at IS NULL AND scope = 'identifier';

CREATE UNIQUE INDEX datasource_blocks_datasource_idx
    ON datasource_blocks (datasource, kind)
    WHERE cleared_at IS NULL AND scope = 'datasource';

CREATE INDEX datasource_blocks_open_idx
    ON datasource_blocks (datasource, kind) WHERE cleared_at IS NULL;
