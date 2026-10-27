-- +goose Up

-- What an identifier event did to a ticker: 'assigned' gave it to an
-- instrument, 'renamed' moved its instrument to to_ticker_id, and 'retired'
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

-- The composite foreign keys on via_market make the venue of a dated
-- association a member of its ticker's market.
ALTER TABLE stated_keys
    ADD COLUMN via_ticker_id uuid,
    ADD COLUMN via_market    text,
    ADD COLUMN via_venue     text,
    ADD FOREIGN KEY (via_ticker_id, via_market) REFERENCES tickers (id, market),
    ADD FOREIGN KEY (via_venue, via_market) REFERENCES market_members (operating_mic, market),
    ADD CHECK ((via_ticker_id IS NULL) = (via_market IS NULL)
               AND (via_ticker_id IS NULL) = (via_venue IS NULL)),
    ADD CHECK (instr_id IS NULL OR via_ticker_id IS NULL),
    ADD CHECK ((arbiter IS NULL) = (instr_id IS NULL AND via_ticker_id IS NULL));

-- An identifier assertion is a claim that a ticker names an instrument, or
-- names nothing, on one day. A datasource asserts through a fetch key, on the
-- day of its answer. A point in time lookup asserts on the day its request
-- names. An entity's ticker history asserts each ticker on the day the ticker
-- began naming the entity. An administrator asserts through an attest
-- identifier run, on any day up to the day of the identifier attestation. A
-- row goes with its source.
CREATE TABLE identifier_assertions (
    id              uuid            PRIMARY KEY,
    fetch_key_id    uuid            REFERENCES fetch_keys (id) ON DELETE CASCADE,
    -- run_id is the attest identifier run, and attested_by the administrator
    -- who started it.
    run_id          uuid            REFERENCES runs (id) ON DELETE CASCADE,
    attested_by     uuid            REFERENCES users (id),
    ticker_id       uuid            NOT NULL,
    market          text            NOT NULL,
    -- instr_id is NULL for a negative assertion: the ticker names nothing at
    -- the source.
    instr_id        uuid            REFERENCES instrs (id),
    asserted_on     date            NOT NULL,
    CHECK ((fetch_key_id IS NULL) <> (run_id IS NULL)),
    CHECK ((run_id IS NULL) = (attested_by IS NULL)),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market),
    UNIQUE (fetch_key_id, ticker_id, asserted_on)
);

CREATE INDEX identifier_assertions_ticker_idx ON identifier_assertions (ticker_id);
-- An administrator attests a day once: a later identifier attestation replaces
-- the earlier one.
CREATE UNIQUE INDEX identifier_attestations_day_idx ON identifier_assertions (ticker_id, asserted_on)
    WHERE run_id IS NOT NULL;
CREATE INDEX identifier_assertions_run_idx ON identifier_assertions (run_id);
CREATE INDEX identifier_assertions_instr_idx ON identifier_assertions (instr_id);

-- An identifier event is a change a datasource reported in what a ticker
-- names, taking effect at the start of effective_on. An event refers to a
-- ticker rather than to an instrument. The table holds only the events a
-- datasource reported. Readers derive any event the assertions imply.
CREATE TABLE identifier_events (
    id           uuid                    PRIMARY KEY,
    fetch_key_id uuid                    NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    market       text                    NOT NULL,
    ticker_id    uuid                    NOT NULL,
    action       identifier_event_action NOT NULL,
    -- to_ticker_id is the new ticker of a rename, in the same market.
    to_ticker_id uuid,
    effective_on date                    NOT NULL,
    CHECK ((action = 'renamed') = (to_ticker_id IS NOT NULL)),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market),
    FOREIGN KEY (to_ticker_id, market) REFERENCES tickers (id, market)
);

CREATE INDEX identifier_events_ticker_idx ON identifier_events (ticker_id);
CREATE INDEX identifier_events_to_ticker_idx ON identifier_events (to_ticker_id);
CREATE INDEX identifier_events_fetch_key_idx ON identifier_events (fetch_key_id);

-- Identifier event coverage is a period over which a datasource reported
-- every identifier event of one ticker, or of every ticker in a market where
-- ticker_id is NULL. Inside it, the absence of an event is a claim that none
-- occurred. A row goes with its fetch key.
CREATE TABLE identifier_event_coverage (
    id             uuid PRIMARY KEY,
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    market         text NOT NULL REFERENCES markets (code),
    ticker_id      uuid,
    covered_from   date NOT NULL,
    covered_before date NOT NULL,
    CHECK (covered_from < covered_before),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market)
);

CREATE INDEX identifier_event_coverage_ticker_idx ON identifier_event_coverage (market, ticker_id);
CREATE INDEX identifier_event_coverage_fetch_key_idx ON identifier_event_coverage (fetch_key_id);
