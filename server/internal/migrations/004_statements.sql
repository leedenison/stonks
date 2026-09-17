-- +goose Up

-- A statement is a run of kind 'statement': one batch of transactions a user
-- submitted in the neutral format, claiming every account they hold at one
-- broker over a half-open range of order dates. The row is keyed by the run
-- and exists from receipt, so a pending statement lists with its broker and
-- period. row_count is the number of rows received; those not recorded as
-- items were written as transactions.
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
-- A resolution key is the item row of a run of kind 'resolution': the outcome
-- for one stated key. 'matched' names an existing listing; 'created' names
-- the instrument and listing the resolution made; 'rejected' names nothing and
-- carries the reason.

CREATE TABLE statements (
    id           uuid        PRIMARY KEY REFERENCES runs (id),
    user_id      uuid        NOT NULL REFERENCES users (id),
    broker       broker      NOT NULL,
    order_from   date        NOT NULL,
    order_before date        NOT NULL,
    row_count    integer     NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (id, user_id) REFERENCES runs (id, user_id),
    CHECK (order_from < order_before),
    CHECK (row_count >= 0)
);

CREATE INDEX statements_user_idx ON statements (user_id, id);

CREATE TABLE statement_items (
    statement_id  uuid        NOT NULL,
    user_id    uuid        NOT NULL,
    ordinal    integer     NOT NULL,
    reason     text        NOT NULL,
    stated     jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES runs (id, user_id),
    CHECK (jsonb_typeof(stated) = 'object')
);

CREATE TABLE statement_splits (
    statement_id      uuid        NOT NULL,
    user_id        uuid        NOT NULL,
    ordinal        integer     NOT NULL,
    stated_key_id  uuid        NOT NULL REFERENCES stated_keys (id),
    effective_date date        NOT NULL,
    quantity       numeric     NOT NULL,
    ratio_from     numeric,
    ratio_to       numeric,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (statement_id, ordinal),
    FOREIGN KEY (statement_id, user_id) REFERENCES runs (id, user_id),
    CHECK ((ratio_from IS NULL) = (ratio_to IS NULL))
);

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
