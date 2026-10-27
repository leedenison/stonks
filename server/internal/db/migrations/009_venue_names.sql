-- +goose Up

-- The common name of each venue that can name a holding, and a rank for
-- choosing which of a listing's venues names it where no datasource stated
-- the listing's primary venue. A venue outside this table is shown by its
-- MIC and ranks after every venue in it.
CREATE TABLE venue_names (
    mic  text    PRIMARY KEY REFERENCES mics (mic),
    name text    NOT NULL,
    rank integer NOT NULL UNIQUE
);

INSERT INTO venue_names (mic, name, rank) VALUES
    ('XNYS', 'NYSE', 1),
    ('XNAS', 'Nasdaq', 2),
    ('XLON', 'LSE', 3),
    ('XFRA', 'Xetra', 4),
    ('XPAR', 'Euronext Paris', 5),
    ('XAMS', 'Euronext Amsterdam', 6),
    ('XBRU', 'Euronext Brussels', 7),
    ('XLIS', 'Euronext Lisbon', 8),
    ('XDUB', 'Euronext Dublin', 9),
    ('XMIL', 'Borsa Italiana', 10),
    ('BMEX', 'BME', 11),
    ('XSWX', 'SIX', 12),
    ('XSTO', 'Nasdaq Stockholm', 13),
    ('XCSE', 'Nasdaq Copenhagen', 14),
    ('XHEL', 'Nasdaq Helsinki', 15),
    ('XOSL', 'Oslo Bors', 16),
    ('XWBO', 'Wiener Boerse', 17),
    ('XTSE', 'TSX', 18),
    ('XTSX', 'TSXV', 19),
    ('XASX', 'ASX', 20),
    ('XJPX', 'Tokyo', 21),
    ('XHKG', 'HKEX', 22),
    ('XSES', 'SGX', 23),
    ('XJSE', 'JSE', 24),
    ('XCBO', 'Cboe', 26),
    ('IEXG', 'IEX', 27);

-- listing_tickers is each listing's tickers ranked as its name: the primary
-- venue first, then by venue_names.rank, then by MIC. is_primary and
-- venue_rank carry the order so a query over several listings can rank
-- across them.
CREATE VIEW listing_tickers AS
SELECT listings.id AS listing_id, listings.instr_id, listings.currency,
       identifiers.domain, identifiers.value,
       (identifiers.domain = listings.primary_mic) AS is_primary,
       venue_names.rank AS venue_rank,
       ROW_NUMBER() OVER (PARTITION BY listings.id
           ORDER BY (identifiers.domain = listings.primary_mic) DESC NULLS LAST,
                    venue_names.rank NULLS LAST, identifiers.domain) AS rank
FROM listings
JOIN identifiers ON identifiers.listing_id = listings.id AND identifiers.type = 'mic_ticker'
LEFT JOIN venue_names ON venue_names.mic = identifiers.domain;

-- instr_names gives each instrument the one identifier that names it,
-- chosen from its own identifiers and those of its listings. Types rank by
-- name_rank. Among tickers the primary venue wins, then venue_names.rank,
-- then domain and value. A row exists only for an instrument with an
-- identifier.
CREATE VIEW instr_names AS
SELECT DISTINCT ON (identifiers.instr_id)
       identifiers.instr_id, identifiers.type, identifiers.domain, identifiers.value
FROM identifiers
JOIN identifier_type_traits ON identifier_type_traits.type = identifiers.type
LEFT JOIN listing_tickers ON listing_tickers.listing_id = identifiers.listing_id
    AND listing_tickers.domain = identifiers.domain AND listing_tickers.value = identifiers.value
ORDER BY identifiers.instr_id, identifier_type_traits.name_rank,
         listing_tickers.is_primary DESC NULLS LAST, listing_tickers.venue_rank NULLS LAST,
         identifiers.domain, identifiers.value;
