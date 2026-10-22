-- +goose Up

-- What an identifier event did to a MIC_TICKER value: 'assigned' gave it to
-- an instrument, 'renamed' moved its instrument to to_value, and 'retired'
-- left it naming nothing.
CREATE TYPE identifier_event_action AS ENUM ('assigned', 'renamed', 'retired');

-- An identifier assertion is a claim that an identifier names an instrument,
-- or names nothing, on one day. A datasource asserts through a fetch key, on
-- the day of its answer. A point in time lookup asserts on the day its request
-- names. An entity's ticker history asserts each ticker on the day the ticker
-- began naming the entity. An administrator asserts through an attestation
-- run, on any day up to the day of the attestation. A row goes with its
-- source.
CREATE TABLE identifier_assertions (
    id              uuid            PRIMARY KEY,
    fetch_key_id    uuid            REFERENCES fetch_keys (id) ON DELETE CASCADE,
    -- run_id is the attestation run, and attested_by the administrator who
    -- started it.
    run_id          uuid            REFERENCES runs (id) ON DELETE CASCADE,
    attested_by     uuid            REFERENCES users (id),
    type            identifier_type NOT NULL,
    domain          text            NOT NULL DEFAULT '',
    value           text            NOT NULL,
    -- instrument_id is NULL for a negative assertion: the identifier names
    -- nothing at the source.
    instrument_id   uuid            REFERENCES instruments (id),
    asserted_on     date            NOT NULL,
    CHECK ((fetch_key_id IS NULL) <> (run_id IS NULL)),
    CHECK ((run_id IS NULL) = (attested_by IS NULL)),
    UNIQUE (fetch_key_id, type, domain, value, asserted_on)
);

CREATE INDEX identifier_assertions_value_idx ON identifier_assertions (type, domain, value);
CREATE INDEX identifier_assertions_run_idx ON identifier_assertions (run_id);
CREATE INDEX identifier_assertions_instrument_idx ON identifier_assertions (instrument_id);

-- An identifier event is a change a datasource reported in what a MIC_TICKER
-- value names, taking effect at the start of effective_on. The domain is the
-- operating MIC. An event refers to a value rather than to an instrument. The
-- table holds only the events a datasource reported. Readers derive any event
-- the assertions imply.
CREATE TABLE identifier_events (
    id           uuid                    PRIMARY KEY,
    fetch_key_id uuid                    NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    domain       text                    NOT NULL,
    value        text                    NOT NULL,
    action       identifier_event_action NOT NULL,
    to_value     text,
    effective_on date                    NOT NULL,
    CHECK (domain <> ''),
    CHECK ((action = 'renamed') = (to_value IS NOT NULL))
);

CREATE INDEX identifier_events_value_idx ON identifier_events (domain, value);
CREATE INDEX identifier_events_to_value_idx ON identifier_events (domain, to_value);
CREATE INDEX identifier_events_fetch_key_idx ON identifier_events (fetch_key_id);

-- Identifier event coverage is a period over which a datasource reported
-- every identifier event of one MIC_TICKER value, or of every value in a
-- domain where value is NULL. Inside it, the absence of an event is a claim
-- that none occurred. A row goes with its fetch key.
CREATE TABLE identifier_event_coverage (
    id             uuid PRIMARY KEY,
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    domain         text NOT NULL,
    value          text,
    covered_from   date NOT NULL,
    covered_before date NOT NULL,
    CHECK (domain <> ''),
    CHECK (covered_from < covered_before)
);

CREATE INDEX identifier_event_coverage_value_idx ON identifier_event_coverage (domain, value);
CREATE INDEX identifier_event_coverage_fetch_key_idx ON identifier_event_coverage (fetch_key_id);
