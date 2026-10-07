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
    CHECK (name <> ''),
    -- Deferred, so a reorder that swaps two precedences passes. A deferrable
    -- constraint cannot arbitrate ON CONFLICT, so no upsert may name it.
    UNIQUE (precedence) DEFERRABLE INITIALLY DEFERRED
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
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    datasource text        NOT NULL REFERENCES datasources (name),
    kind       fetch_kind  NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id) ON DELETE CASCADE,
    UNIQUE (user_id, id)
);

-- A fetch key is one fetched identifier within a fetch covering
-- several identifiers.
CREATE TABLE fetch_keys (
    id            uuid            PRIMARY KEY,
    fetch_id      uuid            NOT NULL,
    user_id       uuid            NOT NULL,
    stated_key_id uuid            NOT NULL REFERENCES stated_keys (id) ON DELETE CASCADE,
    outcome       fetch_outcome   NOT NULL,
    attempts      smallint        NOT NULL,
    sent_type     identifier_type,
    sent_domain   text            NOT NULL DEFAULT '',
    sent_value    text,
    instrument_id uuid            REFERENCES instruments (id),
    -- reason records the datasource's error when results are not returned.
    reason        text,
    -- candidates is how many results the datasource offered; zero unless served.
    candidates    smallint        NOT NULL DEFAULT 0,
    created_at    timestamptz     NOT NULL DEFAULT now(),
    FOREIGN KEY (fetch_id, user_id) REFERENCES fetches (id, user_id) ON DELETE CASCADE,
    UNIQUE (fetch_id, stated_key_id),
    CHECK ((outcome = 'not_served') = (sent_type IS NULL)),
    CHECK ((sent_type IS NULL) = (sent_value IS NULL)),
    CHECK (sent_type IS NOT NULL OR sent_domain = ''),
    CHECK ((outcome = 'served') = (reason IS NULL)),
    CHECK ((outcome IN ('not_served', 'blocked')) = (attempts = 0)),
    CHECK (instrument_id IS NULL OR outcome = 'served'),
    CHECK (outcome = 'served' OR candidates = 0)
);

CREATE INDEX fetch_keys_stated_key_idx ON fetch_keys (stated_key_id);
CREATE INDEX fetch_keys_instrument_idx ON fetch_keys (instrument_id);

-- Provenance: the fetch key whose response asserted the row, NULL for
-- reference data. Deleting a fetch key does not delete what it asserted, so
-- the caller first moves the provenance elsewhere or clears it.
ALTER TABLE instruments ADD COLUMN fetch_key_id uuid REFERENCES fetch_keys (id);
ALTER TABLE listings ADD COLUMN fetch_key_id uuid REFERENCES fetch_keys (id);
ALTER TABLE identifiers ADD COLUMN fetch_key_id uuid REFERENCES fetch_keys (id);

-- Identity coverage: the datasource has answered for the instrument. Either
-- its answer attached to the instrument, or it served no candidate for a key
-- the instrument names. It is not requested again for a key that resolves
-- to the instrument. An identity response
-- holds at the moment of the fetch, so the row carries that moment and the
-- fetch key. Reference data, the instruments with no provenance, is covered
-- by every datasource without a row. A row goes with its fetch key.
CREATE TABLE identity_coverage (
    instrument_id uuid        NOT NULL REFERENCES instruments (id),
    datasource    text        NOT NULL REFERENCES datasources (name),
    fetch_key_id  uuid        NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    covered_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (instrument_id, datasource)
);

-- The identifiers returned as part of a fetch result; an assertion by the
-- datasource that the identifiers refer to the same instrument at the time
-- of the fetch.
CREATE TABLE fetch_identifiers (
    fetch_key_id uuid            NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    type         identifier_type NOT NULL,
    domain       text            NOT NULL DEFAULT '',
    value        text            NOT NULL,
    created_at   timestamptz     NOT NULL DEFAULT now(),
    UNIQUE (fetch_key_id, type, domain, value)
);

CREATE INDEX fetch_identifiers_value_idx ON fetch_identifiers (type, domain, value);

-- A block records that a call failed unrecoverably, and suppresses further calls
-- until an administrator clears it. A block goes with the fetch key whose call
-- created it.
CREATE TABLE datasource_blocks (
    id           uuid        PRIMARY KEY,
    datasource   text        NOT NULL REFERENCES datasources (name),
    kind         fetch_kind  NOT NULL,
    scope        block_scope NOT NULL,
    sent_type    identifier_type,
    sent_domain  text        NOT NULL DEFAULT '',
    sent_value   text,
    reason       text        NOT NULL,
    -- fetch_key_id is the call that created the block.
    fetch_key_id uuid        NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    cleared_at   timestamptz,
    CHECK ((scope = 'identifier') = (sent_type IS NOT NULL)),
    CHECK ((sent_type IS NULL) = (sent_value IS NULL)),
    CHECK (sent_type IS NOT NULL OR sent_domain = '')
);

CREATE UNIQUE INDEX datasource_blocks_identifier_idx
    ON datasource_blocks (datasource, kind, sent_type, sent_domain, sent_value)
    WHERE cleared_at IS NULL AND scope = 'identifier';

CREATE UNIQUE INDEX datasource_blocks_datasource_idx
    ON datasource_blocks (datasource, kind)
    WHERE cleared_at IS NULL AND scope = 'datasource';

CREATE INDEX datasource_blocks_open_idx
    ON datasource_blocks (datasource, kind) WHERE cleared_at IS NULL;
