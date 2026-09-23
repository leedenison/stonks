-- +goose Up

CREATE TYPE broker AS ENUM ('ibkr', 'schwab', 'fidelity_uk');

-- A statement is a run of kind 'statement': one batch of transactions a user
-- submitted in the neutral format, claiming every account they hold at one
-- broker over a half-open range of order dates.
--  
-- The row is keyed by the run and exists from receipt.
CREATE TABLE statements (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id),
    broker       broker      NOT NULL,
    order_from   date        NOT NULL,
    order_before date        NOT NULL,
    row_count    integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id),
    UNIQUE (user_id, id),
    CHECK (order_from < order_before),
    CHECK (row_count >= 0)
);

CREATE TYPE validity AS ENUM ('confirmed', 'provisional');

-- A stated key is what one source states about an instrument: its
-- identifiers, asset class, currency and description. It is stored with the
-- statement that carried it and is what parameterizes resolution requests.
-- The identifiers it states become identifier rows only when confirmed by a
-- datasource.
CREATE TABLE stated_keys (
    id            uuid        PRIMARY KEY,
    statement_id  uuid        NOT NULL,
    user_id       uuid        NOT NULL,
    asset_class   asset_class REFERENCES asset_class_tree (class),
    currency      text,
    description   text,
    -- identifiers is a JSON array of objects with type, value and, when
    -- stated, a domain, sorted by type, domain and value with no domain
    -- sorting first, so that two statements of one key compare equal. The
    -- whole key is the unique index, which bounds it at one btree entry
    -- (about 2.7KB).
    identifiers   jsonb       NOT NULL DEFAULT '[]',
    -- instrument_id references the instrument this stated_key resolved to.
    instrument_id uuid        REFERENCES instruments (id),
    -- listing_id references the listing this stated_key resolved to, if known.
    listing_id    uuid        REFERENCES listings (id),
    -- via_id references the identifier used to make the instrument/listing
    -- association.
    via_id        uuid        REFERENCES identifiers (id),
    -- validity indicates whether the association is 'confirmed', either
    -- because the identifier is stable or we have identifier event coverage
    -- that includes the resolution time.  Otherwise validity is marked
    -- 'provisional'.
    validity      validity,
    -- group_id gathers the unresolved keys which share an identifier, or a
    -- description transitively within one broker.
    group_id      uuid        REFERENCES stated_keys (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(identifiers) = 'array'),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id),
    UNIQUE NULLS NOT DISTINCT (statement_id, asset_class, currency, description, identifiers),
    CHECK ((instrument_id IS NULL) = (via_id IS NULL)),
    CHECK ((instrument_id IS NULL) = (validity IS NULL)),
    CHECK (listing_id IS NULL OR instrument_id IS NOT NULL),
    CHECK (group_id IS NULL OR instrument_id IS NULL)
);

CREATE INDEX stated_keys_instrument_idx ON stated_keys (user_id, instrument_id);
CREATE INDEX stated_keys_group_idx ON stated_keys (user_id, group_id);

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
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    CHECK (jsonb_typeof(stated) = 'object')
);

-- A statement split records stock splits included in ingested statements.
CREATE TABLE statement_splits (
    statement_id   uuid        NOT NULL,
    user_id        uuid        NOT NULL,
    ordinal        integer     NOT NULL,
    stated_key_id  uuid        NOT NULL REFERENCES stated_keys (id),
    effective_date date        NOT NULL,
    quantity       numeric     NOT NULL,
    -- ratio_from is the change in units before the split.
    ratio_from     numeric,
    -- ratio_to is the change in units after the split.
    ratio_to       numeric,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    CHECK ((ratio_from IS NULL) = (ratio_to IS NULL))
);

-- A transaction is a change in the quantity of one instrument held by a user,
-- with an order date and a settlement date, stated as at a date: the date on
-- which its values were true, and so which corporate events they reflect. Its
-- instrument and listing are its key's.
--
-- Replacement is keyed on user, broker and order date, so those are columns of
-- the row rather than reached through the statement.
--
-- quantity is in units of the instrument: shares, contracts, or money for
-- cash. The currency it is stated in is the key's.
CREATE TABLE transactions (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id),
    broker          broker      NOT NULL,
    statement_id    uuid        NOT NULL,
    stated_key_id   uuid        NOT NULL REFERENCES stated_keys (id),
    order_date      date        NOT NULL,
    settlement_date date        NOT NULL,
    as_at           date        NOT NULL,
    quantity        numeric     NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id)
);

CREATE INDEX transactions_period_idx ON transactions (user_id, broker, order_date);
CREATE INDEX transactions_key_idx ON transactions (user_id, stated_key_id);

CREATE TYPE resolution_outcome AS ENUM ('matched', 'rejected', 'unresolved');

-- A resolution key is the item row of a run of kind 'resolution': the outcome
-- for one stated key.
CREATE TABLE resolution_keys (
    run_id        uuid               NOT NULL,
    user_id       uuid               NOT NULL,
    stated_key_id uuid               NOT NULL REFERENCES stated_keys (id),
    -- outcome is one of: 'matched' - the run resolved the key; 'unresolved' the
    -- run failed to resolve the key; 'rejected' the run could not attempt to
    -- resolve the key.
    outcome       resolution_outcome NOT NULL,
    -- reason carries the reason for rejecting a key.
    reason        text,
    created_at    timestamptz        NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, stated_key_id),
    FOREIGN KEY (run_id, user_id) REFERENCES runs (id, user_id),
    CHECK ((outcome = 'rejected') = (reason IS NOT NULL))
);
