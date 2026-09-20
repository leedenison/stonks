-- +goose Up

-- A statement is a run of kind 'statement': one batch of transactions a user
-- submitted in the neutral format, claiming every account they hold at one
-- broker over a half-open range of order dates. The row is keyed by the run
-- and exists from receipt, so a pending statement lists with its broker and
-- period. row_count is the number of rows received; those not recorded as
-- items were written as transactions. Every row a statement leaves names
-- the statement and its user together, so a link cannot cross users or name
-- a run of another kind.
--
-- A stated key is what one source states about an instrument: its
-- identifiers, asset class, currency and description. It is stored with the
-- statement that carried it, one row per distinct key, and is what resolution
-- answers. The identifiers it states become identifier rows only when
-- resolution admits them.
--
-- A statement item is a row the statement rejected: its position in the
-- payload, why, and the row as stated, as protobuf JSON. An accepted row is
-- recorded as its transaction and has no item.
--
-- A statement split is a split as the export stated it, recorded against the
-- statement and never applied. ratio_from and ratio_to are the change in units
-- between old and new, 1 and 10 for a ten-for-one split, and are NULL
-- together when the export stated no ratio.
--
-- A transaction is a change in the quantity of one instrument held by a user,
-- with an order date and a settlement date, stated as at a date: the date on
-- which its values were true, and so which corporate events they reflect.
-- Replacement is keyed on user, broker and order date, so those are columns of
-- the row rather than reached through the statement.
--
-- A resolution key is the item row of a run of kind 'resolution': the outcome
-- for one stated key. 'matched' names an existing listing; 'created' names
-- the instrument and listing the resolution made; 'rejected' names nothing and
-- carries the reason.

CREATE TYPE broker AS ENUM ('ibkr', 'schwab', 'fidelity_uk');

-- The unique constraint on (user_id, id) is what the links from the rows
-- below reference, and its index serves the list by user in id order.
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

-- identifiers is a JSON array of objects with type, value and, when stated, a
-- domain, sorted by type, domain and value with no domain sorting first, so
-- that two statements of one key compare equal. The whole key is the unique
-- index, which bounds it at one btree entry, about 2.7KB.
--
-- currency is the code as the source stated it, checked against nothing.
CREATE TABLE stated_keys (
    id           uuid        PRIMARY KEY,
    statement_id uuid        NOT NULL,
    user_id      uuid        NOT NULL,
    asset_class  asset_class REFERENCES asset_class_tree (class),
    currency     text,
    description  text,
    identifiers  jsonb       NOT NULL DEFAULT '[]',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(identifiers) = 'array'),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    UNIQUE NULLS NOT DISTINCT (statement_id, asset_class, currency, description, identifiers)
);

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

CREATE TABLE statement_splits (
    statement_id   uuid        NOT NULL,
    user_id        uuid        NOT NULL,
    ordinal        integer     NOT NULL,
    stated_key_id  uuid        NOT NULL REFERENCES stated_keys (id),
    effective_date date        NOT NULL,
    quantity       numeric     NOT NULL,
    ratio_from     numeric,
    ratio_to       numeric,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    CHECK ((ratio_from IS NULL) = (ratio_to IS NULL))
);

-- listing_id is set when resolution named a listing, and currency when the
-- source stated one, as the precise code stated. quantity is in units of the
-- instrument: shares, contracts, or money for cash.
CREATE TABLE transactions (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id),
    broker          broker      NOT NULL,
    statement_id    uuid        NOT NULL,
    stated_key_id   uuid        NOT NULL REFERENCES stated_keys (id),
    instrument_id   uuid        NOT NULL REFERENCES instruments (id),
    listing_id      uuid        REFERENCES listings (id),
    order_date      date        NOT NULL,
    settlement_date date        NOT NULL,
    as_at           date        NOT NULL,
    quantity        numeric     NOT NULL,
    currency        text        REFERENCES currencies (code),
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (statement_id, user_id) REFERENCES statements (id, user_id),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id)
);

CREATE INDEX transactions_period_idx ON transactions (user_id, broker, order_date);
CREATE INDEX transactions_instrument_idx ON transactions (user_id, instrument_id, listing_id);

CREATE TYPE resolution_outcome AS ENUM ('matched', 'created', 'rejected');

CREATE TABLE resolution_keys (
    run_id        uuid               NOT NULL,
    user_id       uuid               NOT NULL,
    stated_key_id uuid               NOT NULL REFERENCES stated_keys (id),
    outcome       resolution_outcome NOT NULL,
    instrument_id uuid               REFERENCES instruments (id),
    listing_id    uuid               REFERENCES listings (id),
    reason        text,
    created_at    timestamptz        NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, stated_key_id),
    FOREIGN KEY (run_id, user_id) REFERENCES runs (id, user_id),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id),
    CHECK ((outcome = 'rejected') = (reason IS NOT NULL)),
    CHECK ((outcome = 'rejected') = (instrument_id IS NULL))
);
