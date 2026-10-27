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
    -- venue and currency are what the source stated beside the ticker: the
    -- operating MIC of the venue, and the currency of the line. Each is NULL
    -- where the source stated none. EODHD states the venue and currency of
    -- its row. OpenFIGI states the venue of its exchange code, and omits the
    -- currency, so its integration stamps the currency of the request's
    -- filter. A ticker history, a point in time lookup and an identifier
    -- attestation state neither. Ticker validity ignores both, and
    -- resolved_tickers reads them.
    venue           text,
    currency        text            REFERENCES currencies (code),
    CHECK ((fetch_key_id IS NULL) <> (run_id IS NULL)),
    CHECK ((run_id IS NULL) = (attested_by IS NULL)),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market),
    FOREIGN KEY (venue, market) REFERENCES market_members (operating_mic, market),
    -- edge is part of the key, so a source can state a first and a last for
    -- one ticker on one day. venue is part of the key, so an answer that lists
    -- the ticker at several venues of its market writes one assertion per
    -- venue.
    UNIQUE NULLS NOT DISTINCT (fetch_key_id, ticker_id, asserted_on, edge, venue)
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

-- A ticker relates to an instrument it named. 'current': it still names the
-- instrument. 'defunct': it now names another instrument. 'former': it names
-- nothing now.
CREATE TYPE ticker_relation AS ENUM ('current', 'defunct', 'former');

-- resolved_tickers names each instrument at each market by one ticker: of the
-- tickers whose ticker validity names the instrument, the one whose last
-- interval ends latest. An open interval ends latest. A tie goes to the ticker
-- that most recently won a day naming the instrument, then to the lower
-- value. valid_from and valid_before bound the unbroken run of intervals that
-- ends with the last one. A NULL bound is open, so a NULL valid_before means
-- the ticker still names the instrument.
--
-- Each venue that an assertion of the ticker and instrument states gets a row.
-- Its currency is that of the latest assertion at the venue that states one.
-- Where every assertion omits the venue, one row has a NULL venue.
--
-- The table holds names only, and ticker validity stays in the facts. Only
-- tickervalidity.Rebuild writes the table.
CREATE TABLE resolved_tickers (
    ticker_id    uuid NOT NULL,
    market       text NOT NULL,
    instr_id     uuid NOT NULL REFERENCES instrs (id) ON DELETE CASCADE,
    venue        text,
    valid_from   date,
    valid_before date,
    currency     text REFERENCES currencies (code),
    FOREIGN KEY (ticker_id, market) REFERENCES tickers (id, market),
    FOREIGN KEY (venue, market) REFERENCES market_members (operating_mic, market),
    CHECK (valid_from IS NULL OR valid_before IS NULL OR valid_from < valid_before),
    UNIQUE NULLS NOT DISTINCT (instr_id, market, venue)
);

CREATE INDEX resolved_tickers_ticker_idx ON resolved_tickers (ticker_id);

-- instr_tickers relates each instrument to each ticker that names it in a row
-- of resolved_tickers or in a positive assertion. A ticker is defunct where
-- another instrument has an open row of it. A ticker that still names an
-- instrument only through a tie it lost lacks a row for that instrument. It
-- then reads as former on the instrument it named before.
CREATE VIEW instr_tickers AS
SELECT named.ticker_id, named.instr_id,
       (CASE
            WHEN bool_or(named.open) THEN 'current'
            WHEN EXISTS (SELECT 1 FROM resolved_tickers AS other
                         WHERE other.ticker_id = named.ticker_id
                           AND other.instr_id <> named.instr_id
                           AND other.valid_before IS NULL) THEN 'defunct'
            ELSE 'former'
        END)::ticker_relation AS relation
FROM (
    SELECT ticker_id, instr_id, valid_before IS NULL AS open FROM resolved_tickers
    UNION ALL
    SELECT ticker_id, instr_id, false FROM identifier_assertions WHERE instr_id IS NOT NULL
) AS named
GROUP BY named.ticker_id, named.instr_id;

-- listing_tickers gives each listing every row of resolved_tickers of its
-- instrument whose currency is in the listing's family, ranked as the
-- listing's name: the primary venue first, then by venue_names.rank, then by
-- venue. A row without a currency stays apart from every listing. The join
-- runs on read, so a listing that a later write creates is reached without a
-- rebuild.
-- is_primary and venue_rank carry the order so a query over several listings
-- can rank across them.
CREATE VIEW listing_tickers AS
SELECT listings.id AS listing_id, listings.instr_id, listings.currency,
       resolved_tickers.ticker_id, tickers.market, tickers.value, resolved_tickers.venue,
       instr_tickers.relation,
       (resolved_tickers.venue = listings.primary_mic) AS is_primary,
       venue_names.rank AS venue_rank,
       ROW_NUMBER() OVER (PARTITION BY listings.id
           ORDER BY (resolved_tickers.venue = listings.primary_mic) DESC NULLS LAST,
                    venue_names.rank NULLS LAST, resolved_tickers.venue NULLS LAST,
                    tickers.market) AS rank
FROM listings
JOIN resolved_tickers ON resolved_tickers.instr_id = listings.instr_id
JOIN currencies ON currencies.code = resolved_tickers.currency
    AND currencies.family = listings.currency
JOIN tickers ON tickers.id = resolved_tickers.ticker_id
JOIN instr_tickers ON instr_tickers.ticker_id = resolved_tickers.ticker_id
    AND instr_tickers.instr_id = resolved_tickers.instr_id
LEFT JOIN venue_names ON venue_names.mic = resolved_tickers.venue;

-- instr_names gives each instrument the one identifier that names it. It
-- chooses from the instrument's identifiers other than its MIC_TICKERs, and
-- from its rows of resolved_tickers that state a venue and are not defunct,
-- each as a MIC_TICKER at that venue. A row without a venue is left out,
-- since the domain of a MIC_TICKER is an operating MIC. A defunct ticker is
-- left out too, since it names another instrument now. Types rank by
-- name_rank. Among tickers a current one wins, then the primary venue of a
-- listing, then venue_names.rank, then domain and value. A row exists only
-- for an instrument with a name.
CREATE VIEW instr_names AS
SELECT DISTINCT ON (named.instr_id) named.instr_id, named.type, named.domain, named.value
FROM (
    SELECT identifiers.instr_id, identifiers.type, identifiers.domain, identifiers.value,
           identifier_type_traits.name_rank, NULL::boolean AS is_current,
           NULL::boolean AS is_primary, NULL::integer AS venue_rank
    FROM identifiers
    JOIN identifier_type_traits ON identifier_type_traits.type = identifiers.type
    WHERE identifiers.type <> 'mic_ticker'
    UNION ALL
    SELECT resolved_tickers.instr_id, traits.type, resolved_tickers.venue, tickers.value,
           traits.name_rank, instr_tickers.relation = 'current',
           EXISTS (SELECT 1 FROM listings
                   WHERE listings.instr_id = resolved_tickers.instr_id
                     AND listings.primary_mic = resolved_tickers.venue),
           venue_names.rank
    FROM resolved_tickers
    JOIN tickers ON tickers.id = resolved_tickers.ticker_id
    JOIN instr_tickers ON instr_tickers.ticker_id = resolved_tickers.ticker_id
        AND instr_tickers.instr_id = resolved_tickers.instr_id
    JOIN identifier_type_traits AS traits ON traits.type = 'mic_ticker'
    LEFT JOIN venue_names ON venue_names.mic = resolved_tickers.venue
    WHERE resolved_tickers.venue IS NOT NULL AND instr_tickers.relation <> 'defunct'
) AS named
ORDER BY named.instr_id, named.name_rank, named.is_current DESC NULLS LAST,
         named.is_primary DESC NULLS LAST, named.venue_rank NULLS LAST,
         named.domain, named.value;
