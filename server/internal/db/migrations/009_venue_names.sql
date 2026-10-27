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
