-- +goose Up

-- A span is the days on which a ticker names one thing without a break. An
-- edge marks an assertion's day as the first or last day of a span. 'first'
-- states that the ticker named something else the day before. 'last' states
-- that it names something else the day after. An edge leaves the other side of
-- the change unstated, so a source can record a change it saw from one side.
CREATE TYPE identifier_edge AS ENUM ('first', 'last');

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
-- began naming the entity, with the edge 'first'. It asserts the old ticker of
-- a rename on the day before the rename, with the edge 'last'. An
-- administrator asserts through an attest identifier run, on any day up to the
-- day of the identifier attestation. A row goes with its source.
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
    -- edge is NULL for a point: the days either side stay open.
    edge            identifier_edge,
    CHECK ((fetch_key_id IS NULL) <> (run_id IS NULL)),
    CHECK ((run_id IS NULL) = (attested_by IS NULL)),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market),
    -- edge is part of the key, so a source can state a first and a last for
    -- one ticker on one day.
    UNIQUE NULLS NOT DISTINCT (fetch_key_id, ticker_id, asserted_on, edge)
);

CREATE INDEX identifier_assertions_ticker_idx ON identifier_assertions (ticker_id);
-- An administrator attests a day once: a later identifier attestation replaces
-- the earlier one.
CREATE UNIQUE INDEX identifier_attestations_day_idx ON identifier_assertions (ticker_id, asserted_on)
    WHERE run_id IS NOT NULL;
CREATE INDEX identifier_assertions_run_idx ON identifier_assertions (run_id);
CREATE INDEX identifier_assertions_instr_idx ON identifier_assertions (instr_id);

-- An identifier period assertion is a claim that a datasource reported every
-- edge of one ticker over a period, or of every ticker in a market where
-- ticker_id is NULL. Inside the period, the absence of an edge is a claim
-- that the ticker did not change. A row goes with its fetch key.
CREATE TABLE identifier_period_assertions (
    id              uuid PRIMARY KEY,
    fetch_key_id    uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    market          text NOT NULL REFERENCES markets (code),
    ticker_id       uuid,
    asserted_from   date NOT NULL,
    asserted_before date NOT NULL,
    CHECK (asserted_from < asserted_before),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market)
);

CREATE INDEX identifier_period_assertions_ticker_idx ON identifier_period_assertions (market, ticker_id);
CREATE INDEX identifier_period_assertions_fetch_key_idx ON identifier_period_assertions (fetch_key_id);
