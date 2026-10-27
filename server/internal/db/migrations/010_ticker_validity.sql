-- +goose Up

-- What an identifier event did to a MIC_TICKER: 'assigned' gave it to
-- an instrument, 'renamed' moved its instrument to to_value, and 'retired'
-- left it naming nothing.
CREATE TYPE identifier_event_action AS ENUM ('assigned', 'renamed', 'retired');

-- A market is a set of operating MICs across which one ticker names one
-- instrument on a day. A ticker's identifier facts are keyed by its market.
-- The US listing exchanges share the market US. Every other operating MIC is
-- a market of its own, named by that MIC.
CREATE TABLE markets (
    code text PRIMARY KEY
);

INSERT INTO markets (code) VALUES ('US');
INSERT INTO markets (code)
SELECT DISTINCT operating_mic FROM mics
WHERE operating_mic NOT IN ('BATS', 'EDGE', 'IEXG', 'XCHI', 'XCIS', 'XNAS', 'XNYS');

-- market_members puts every operating MIC in its market.
CREATE TABLE market_members (
    operating_mic text PRIMARY KEY REFERENCES mics (mic),
    market        text NOT NULL REFERENCES markets (code),
    UNIQUE (operating_mic, market)
);

INSERT INTO market_members (operating_mic, market) VALUES
    ('BATS', 'US'),
    ('EDGE', 'US'),
    ('IEXG', 'US'),
    ('XCHI', 'US'),
    ('XCIS', 'US'),
    ('XNAS', 'US'),
    ('XNYS', 'US');
INSERT INTO market_members (operating_mic, market)
SELECT DISTINCT operating_mic, operating_mic FROM mics
WHERE operating_mic NOT IN (SELECT operating_mic FROM market_members);

-- mic_markets gives each MIC its operating MIC and its market.
CREATE VIEW mic_markets AS
SELECT mics.mic, mics.operating_mic, market_members.market
FROM mics
JOIN market_members ON market_members.operating_mic = mics.operating_mic;

-- A ticker is a MIC_TICKER at its market, in the system's spelling. The row
-- holds only the spelling, and the facts of the ticker refer to it. A row
-- outlives its facts. The get-or-create function of package tickers writes
-- the table, and it refuses a spelling that its predicate rejects.
CREATE TABLE tickers (
    id     uuid PRIMARY KEY,
    market text NOT NULL REFERENCES markets (code),
    value  text NOT NULL,
    UNIQUE (market, value),
    -- A table that keeps a ticker in its market holds a composite foreign key
    -- to this constraint.
    UNIQUE (id, market)
);

-- An identifier assertion is a claim that an identifier names an instrument, or
-- names nothing, on one day. A MIC_TICKER is asserted at its market, so the
-- domain of a mic_ticker row is a market. A datasource asserts through a fetch
-- key, on the day of its answer. A point in time lookup asserts on the day its
-- request names. An entity's ticker history asserts each ticker on the day the
-- ticker began naming the entity. An administrator asserts through an attest
-- identifier run, on any day up to the day of the identifier attestation. A row
-- goes with its source.
CREATE TABLE identifier_assertions (
    id              uuid            PRIMARY KEY,
    fetch_key_id    uuid            REFERENCES fetch_keys (id) ON DELETE CASCADE,
    -- run_id is the attest identifier run, and attested_by the administrator
    -- who started it.
    run_id          uuid            REFERENCES runs (id) ON DELETE CASCADE,
    attested_by     uuid            REFERENCES users (id),
    type            identifier_type NOT NULL,
    domain          text            NOT NULL DEFAULT '',
    value           text            NOT NULL,
    -- instr_id is NULL for a negative assertion: the identifier names
    -- nothing at the source.
    instr_id   uuid            REFERENCES instrs (id),
    asserted_on     date            NOT NULL,
    CHECK ((fetch_key_id IS NULL) <> (run_id IS NULL)),
    CHECK ((run_id IS NULL) = (attested_by IS NULL)),
    UNIQUE (fetch_key_id, type, domain, value, asserted_on)
);

CREATE INDEX identifier_assertions_value_idx ON identifier_assertions (type, domain, value);
-- An administrator attests a day once: a later identifier attestation replaces
-- the earlier one.
CREATE UNIQUE INDEX identifier_attestations_day_idx ON identifier_assertions (type, domain, value, asserted_on)
    WHERE run_id IS NOT NULL;
CREATE INDEX identifier_assertions_run_idx ON identifier_assertions (run_id);
CREATE INDEX identifier_assertions_instr_idx ON identifier_assertions (instr_id);

-- An identifier event is a change a datasource reported in what a MIC_TICKER
-- names, taking effect at the start of effective_on. An event refers to a
-- ticker at its market rather than to an instrument. The table holds only the
-- events a datasource reported. Readers derive any event the assertions imply.
CREATE TABLE identifier_events (
    id           uuid                    PRIMARY KEY,
    fetch_key_id uuid                    NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    market       text                    NOT NULL,
    value        text                    NOT NULL,
    action       identifier_event_action NOT NULL,
    to_value     text,
    effective_on date                    NOT NULL,
    CHECK (market <> ''),
    CHECK ((action = 'renamed') = (to_value IS NOT NULL))
);

CREATE INDEX identifier_events_value_idx ON identifier_events (market, value);
CREATE INDEX identifier_events_to_value_idx ON identifier_events (market, to_value);
CREATE INDEX identifier_events_fetch_key_idx ON identifier_events (fetch_key_id);

-- Identifier event coverage is a period over which a datasource reported
-- every identifier event of one MIC_TICKER, or of every MIC_TICKER in a
-- market where value is NULL. Inside it, the absence of an event is a claim
-- that none occurred. A row goes with its fetch key.
CREATE TABLE identifier_event_coverage (
    id             uuid PRIMARY KEY,
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    market         text NOT NULL,
    value          text,
    covered_from   date NOT NULL,
    covered_before date NOT NULL,
    CHECK (market <> ''),
    CHECK (covered_from < covered_before)
);

CREATE INDEX identifier_event_coverage_value_idx ON identifier_event_coverage (market, value);
CREATE INDEX identifier_event_coverage_fetch_key_idx ON identifier_event_coverage (fetch_key_id);
