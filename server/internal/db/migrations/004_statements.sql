-- +goose Up

CREATE TYPE broker AS ENUM ('ibkr', 'schwab', 'fidelity_uk');

-- A statement is a run of kind 'ingest_stmt': one batch of transactions a user
-- submitted in the neutral format, claiming every account they hold at one
-- broker over a half-open range of order dates.
--
-- The row is keyed by the run and exists from receipt.
CREATE TABLE statements (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    broker       broker      NOT NULL,
    order_from   date        NOT NULL,
    order_before date        NOT NULL,
    row_count    integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id) ON DELETE CASCADE,
    UNIQUE (user_id, id),
    CHECK (order_from < order_before),
    CHECK (row_count >= 0)
);

-- What settled an association among candidates: 'stated' where the
-- identifiers the key states, read against the database, left one;
-- 'datasource' where a datasource answered for an identifier the key states
-- and left one; 'guess' where a guess's rank settled it; 'user' where the
-- user did, by confirming a candidate for the key.
CREATE TYPE arbiter AS ENUM ('stated', 'datasource', 'guess', 'user');

-- A stated key is what a user's statements state about an instrument: its
-- identifiers, asset class and currency. The user and those three identify
-- it, and the unique index spans them, so every statement of the user that
-- states the key links to one row through statement_keys. It is the unit at
-- which an association is reused. The row keeps a surrogate because six
-- tables reference it and its identifiers column is too wide to reference.
-- The identifiers it states become identifier rows only when confirmed by a
-- datasource.
--
-- An association is fixed or dated. A fixed association names one instrument
-- for all time, in instrument_id and listing_id, and via_id names the
-- identifier row it was made through. The association stands when that row
-- moves to another instrument. A dated association names a MIC_TICKER in
-- the via columns. Each of the key's transactions takes the instrument the
-- ticker names on its as-at date, and that is derived when read, so nothing on
-- the key changes when the ticker's facts do. The resolver dates a key when it
-- associates the key of its own accord through a ticker. An OPENFIGI_TICKER
-- or DATASOURCE_TICKER dates the key through the MIC_TICKER on its listing,
-- and leaves it fixed where that listing has none. A user's choice is fixed.
-- A fixed association is provisional when it used an unstable identifier,
-- such as a broker description or an option symbol. A key the user chose is
-- confirmed.
CREATE TABLE stated_keys (
    id            uuid        PRIMARY KEY,
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    asset_class   asset_class REFERENCES asset_class_tree (class),
    currency      text,
    -- identifiers is a JSON array of objects with type, value and, when
    -- stated, a domain, sorted by type, domain and value with no domain
    -- sorting first, so that two statements of one key compare equal. The
    -- whole key is the unique index, which bounds it at one btree entry
    -- (about 2.7KB).
    identifiers   jsonb       NOT NULL DEFAULT '[]',
    instrument_id uuid        REFERENCES instruments (id),
    listing_id    uuid        REFERENCES listings (id),
    via_id        uuid        REFERENCES identifiers (id),
    via_type      identifier_type,
    via_domain    text,
    via_value     text,
    arbiter       arbiter,
    -- group_id gathers the keys with no instrument which share an identifier,
    -- transitively: the unresolved keys and the dated ones.
    group_id      uuid        REFERENCES stated_keys (id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(identifiers) = 'array'),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id)
        DEFERRABLE INITIALLY IMMEDIATE,
    UNIQUE NULLS NOT DISTINCT (user_id, asset_class, currency, identifiers),
    CHECK ((instrument_id IS NULL) = (via_id IS NULL)),
    CHECK ((via_type IS NULL) = (via_domain IS NULL) AND (via_type IS NULL) = (via_value IS NULL)),
    CHECK (via_type IS NULL OR (via_type = 'mic_ticker' AND via_domain <> '')),
    CHECK (instrument_id IS NULL OR via_type IS NULL),
    CHECK ((arbiter IS NULL) = (instrument_id IS NULL AND via_type IS NULL)),
    CHECK (listing_id IS NULL OR instrument_id IS NOT NULL),
    CHECK (group_id IS NULL OR instrument_id IS NULL)
);

CREATE INDEX stated_keys_instrument_idx ON stated_keys (user_id, instrument_id);
CREATE INDEX stated_keys_group_idx ON stated_keys (user_id, group_id);
-- A new identifier fact finds every user's keys that state its ticker.
CREATE INDEX stated_keys_identifiers_idx ON stated_keys USING gin (identifiers jsonb_path_ops);

-- A statement key links a statement to a key it states. A statement links
-- every key whose rows pass validation, even when the resolution rejected
-- all of the key's rows, so it lists the key with its outcome.
CREATE TABLE statement_keys (
    statement_id  uuid NOT NULL,
    user_id       uuid NOT NULL,
    stated_key_id uuid NOT NULL REFERENCES stated_keys (id) ON DELETE CASCADE,
    PRIMARY KEY (statement_id, stated_key_id),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id) ON DELETE CASCADE
);

CREATE INDEX statement_keys_key_idx ON statement_keys (stated_key_id);

-- A statement item is a row the statement rejected: its position in the
-- payload, why, and the row as stated (as protobuf JSON).
CREATE TABLE statement_items (
    statement_id uuid        NOT NULL,
    user_id      uuid        NOT NULL,
    ordinal      integer     NOT NULL,
    reason       text        NOT NULL,
    stated       jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id) ON DELETE CASCADE,
    CHECK (jsonb_typeof(stated) = 'object')
);

-- A statement split is a split that a statement states. A corporate action
-- fetch checks it against the calendar.
CREATE TABLE statement_splits (
    statement_id   uuid        NOT NULL,
    user_id        uuid        NOT NULL,
    ordinal        integer     NOT NULL,
    stated_key_id  uuid        NOT NULL REFERENCES stated_keys (id) ON DELETE CASCADE,
    effective_date date        NOT NULL,
    quantity       numeric     NOT NULL,
    -- ratio_from is the change in units before the split.
    ratio_from     numeric,
    -- ratio_to is the change in units after the split.
    ratio_to       numeric,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id) ON DELETE CASCADE,
    CHECK ((ratio_from IS NULL) = (ratio_to IS NULL))
);

CREATE INDEX statement_splits_key_idx ON statement_splits (stated_key_id, effective_date);

-- A transaction is a change in the quantity of one instrument held by a user,
-- with an order date and a settlement date, stated as at a date: the date on
-- which its values were true, and so which corporate actions they reflect. Its
-- instrument and listing are its key's. statement_id records the statement
-- that stated it.
--
-- Replacement is keyed on user, broker and order date, so those are columns of
-- the row rather than reached through the statement.
--
-- quantity is in units of the instrument: shares, contracts, or money for
-- cash. Its stated currency is the key's.
CREATE TABLE txns (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    broker          broker      NOT NULL,
    statement_id    uuid        NOT NULL,
    stated_key_id   uuid        NOT NULL REFERENCES stated_keys (id) ON DELETE CASCADE,
    order_date      date        NOT NULL,
    settlement_date date        NOT NULL,
    as_at           date        NOT NULL,
    quantity        numeric     NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id) ON DELETE CASCADE
);

CREATE INDEX txns_period_idx ON txns (user_id, broker, order_date);
CREATE INDEX txns_key_idx ON txns (user_id, stated_key_id);
CREATE INDEX txns_statement_idx ON txns (statement_id);

CREATE TYPE resolution_outcome AS ENUM ('matched', 'rejected', 'unrecognised', 'unavailable');

-- A resolution key is the item row of a run of kind 'resolve_stmt_keys': the
-- outcome for one stated key.
CREATE TABLE resolution_keys (
    run_id        uuid               NOT NULL,
    user_id       uuid               NOT NULL,
    stated_key_id uuid               NOT NULL REFERENCES stated_keys (id) ON DELETE CASCADE,
    -- outcome: 'matched', the key is associated with an instrument;
    -- 'rejected', the key contradicts the reference data it names and its rows
    -- are refused; 'unrecognised', the key was attempted and nothing named it
    -- or every candidate was dropped; 'unavailable', nothing served the key
    -- and a datasource serving it failed or was blocked, which replay re-tries.
    outcome       resolution_outcome NOT NULL,
    -- reasons says why the key did not match. Where datasources were asked,
    -- each has one entry. A rejected key always has at least one entry.
    reasons       text[]             NOT NULL DEFAULT '{}',
    -- offered counts the candidates from which the user may choose. A
    -- candidate is the head set of one datasource and the sets that a run
    -- would attach to it as the winner. The sets are those the resolution
    -- dropped only because the key was sent under a ticker without its venue,
    -- and those that survived a venue ticker whose venue none of them names.
    offered       integer            NOT NULL DEFAULT 0,
    created_at    timestamptz        NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, stated_key_id),
    FOREIGN KEY (run_id, user_id) REFERENCES runs (id, user_id) ON DELETE CASCADE,
    CHECK (outcome <> 'matched' OR cardinality(reasons) = 0),
    CHECK (outcome <> 'rejected' OR cardinality(reasons) > 0),
    CHECK (offered >= 0),
    CHECK (outcome <> 'matched' OR offered = 0)
);
