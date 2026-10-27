-- +goose Up

CREATE TYPE asset_class AS ENUM ('unknown', 'cash', 'security', 'equity', 'stock', 'etf',
    'mutual_fund', 'fixed_income', 'derivative', 'option', 'future');

-- The asset class tree. A leaf is a concrete class and a parent is the set of
-- the leaves below it, so a source states the narrowest class it can defend
-- and an internal node is a legal stored value. unknown indicates the source
-- did not know the asset class.
CREATE TABLE asset_class_tree (
    class  asset_class PRIMARY KEY,
    parent asset_class REFERENCES asset_class_tree (class)
);

INSERT INTO asset_class_tree (class, parent) VALUES
    ('unknown', NULL),
    ('cash', 'unknown'),
    ('security', 'unknown'),
    ('equity', 'security'),
    ('stock', 'equity'),
    ('etf', 'equity'),
    ('mutual_fund', 'equity'),
    ('fixed_income', 'security'),
    ('derivative', 'security'),
    ('option', 'derivative'),
    ('future', 'derivative');

-- The kinds of identifier an instrument or a listing carries:
-- 'isin', 'cusip', 'cins', 'wertpapier' and 'sedol' are the national and
-- international securities numbers;
-- 'openfigi_share_class' and 'openfigi_composite' are the FIGIs OpenFIGI
-- assigns to a share class and to a country-wide composite listing;
-- 'mic_ticker' is a ticker at an operating MIC, in the system's spelling. An
-- integration converts its own spelling of a ticker to one, and drops a
-- ticker that it cannot convert;
-- 'occ' is an OCC option symbol;
-- 'option' is an option symbol in the OCC format, built from the terms a
-- broker states; a datasource names the same contract under 'occ';
-- 'currency' is an ISO 4217 code, the identifier of money;
-- 'datasource_ticker' is a ticker in one datasource's own spelling. It
-- records what a request sent, as EODHD's splits fetch sends BRK-B.US, and
-- a statement never states one;
-- 'broker_id' is an identifier in one broker's own namespace;
-- 'broker_description' is a line's description as one broker states it, the
-- domain being the broker.
CREATE TYPE identifier_type AS ENUM ('isin', 'cusip', 'cins', 'wertpapier',
    'openfigi_share_class', 'sedol', 'openfigi_composite', 'mic_ticker',
    'occ', 'currency', 'datasource_ticker', 'broker_id', 'broker_description', 'option');

-- What is used to qualify the identifier value.
-- 'global' identifier values are not partitioned (their domain is empty).  They
-- are recognised by all parties.
-- 'venue' identifier values are partitioned by ISO 10383 operating MIC. They
-- are recognised by all parties.
-- 'issuer' identifier values are partitioned by their issuer (ie. datasource
-- or broker).  'issuer' identifier values are recognised only by the issuer.
CREATE TYPE identifier_domain AS ENUM ('global', 'venue', 'issuer');
CREATE TYPE identifier_grain AS ENUM ('instr', 'listing');

-- How readily an identifier is reassigned.
-- 'stable' identifiers are assumed to never be reassigned.
-- 'mic_derived' identifiers are reassigned when their source mic_ticker is
-- reassigned.
-- 'unverifiable' identifiers are reassigned at the issuer's discretion, and no
-- source reports a reassignment.
CREATE TYPE identifier_reassignment AS ENUM ('stable', 'mic_derived', 'unverifiable');

-- The traits of each identifier type: how its values are partitioned, what it
-- names, how readily it is reassigned, whether it is exclusive, and its rank
-- for naming an instrument. The server holds a copy that a test checks
-- against this table.
CREATE TABLE identifier_type_traits (
    type         identifier_type         PRIMARY KEY,
    domain       identifier_domain       NOT NULL,
    grain        identifier_grain        NOT NULL,
    reassignment identifier_reassignment NOT NULL,
    -- exclusive is whether a second value of the type, on one instrument or
    -- listing in one domain, contradicts the first. A listing carries one
    -- composite per market and a broker describes one line in several ways,
    -- so those types are not exclusive.
    exclusive    boolean                 NOT NULL,
    -- name_rank ranks the types for naming an instrument, lowest first. It
    -- is unique, so no two types tie.
    name_rank    integer                 NOT NULL UNIQUE,
    UNIQUE (type, grain)
);

-- The types only an option or cash carries, occ, option and currency, have
-- the lowest ranks, so they name the instrument before any type a stock
-- carries.
INSERT INTO identifier_type_traits (type, domain, grain, reassignment, exclusive, name_rank) VALUES
    ('isin',                 'global', 'instr',      'stable',       true,  7),
    ('cusip',                'global', 'instr',      'stable',       true,  8),
    ('cins',                 'global', 'instr',      'stable',       true, 10),
    ('wertpapier',           'global', 'instr',      'stable',       true, 11),
    ('openfigi_share_class', 'global', 'instr',      'stable',       true, 14),
    ('sedol',                'global', 'listing',    'stable',       true,  9),
    ('openfigi_composite',   'global', 'listing',    'stable',       false, 12),
    ('mic_ticker',           'venue',  'listing',    'mic_derived',  true,  4),
    ('occ',                  'global', 'instr',      'mic_derived',  true,  1),
    -- The seed names a cash instrument by every code of its family, which is
    -- one currency at different unit scales. A key states one code, so
    -- currency is exclusive.
    ('currency',             'global', 'instr',      'stable',       true,  3),
    ('datasource_ticker',    'issuer', 'listing',    'mic_derived',  true,  6),
    ('broker_id',            'issuer', 'instr',      'stable',       true, 15),
    ('broker_description',   'issuer', 'instr',      'unverifiable', false,  5),
    ('option',               'global', 'instr',      'mic_derived',  true,  2);

-- The currency codes a source may state: ISO 4217, plus GBX for sterling in
-- pence. A family is the codes of one currency at different unit scales, named
-- by one of them, and is the key of a listing: GBP and GBX are one family,
-- GBP.
CREATE TABLE currencies (
    code   text    PRIMARY KEY,
    family text    NOT NULL REFERENCES currencies (code),
    -- scale is the number of family units in one unit of this currency: 0.01
    -- for GBX.
    scale  numeric NOT NULL DEFAULT 1 CHECK (scale > 0),
    UNIQUE (code, family)
);

INSERT INTO currencies (code, family, scale)
SELECT code, CASE WHEN code = 'GBX' THEN 'GBP' ELSE code END,
       CASE WHEN code = 'GBX' THEN 0.01 ELSE 1 END
FROM (VALUES
    ('USD'), ('EUR'), ('JPY'), ('GBP'), ('GBX'), ('CHF'), ('CNY'), ('AUD'), ('CAD'),
    ('NZD'), ('SEK'), ('NOK'), ('DKK'), ('HKD'), ('SGD'), ('KRW'), ('INR'), ('TWD'),
    ('MXN'), ('BRL'), ('ZAR'), ('TRY'), ('RUB'), ('PLN'), ('CZK'), ('HUF'), ('RON'),
    ('BGN'), ('HRK'), ('AED'), ('SAR'), ('QAR'), ('KWD'), ('BHD'), ('OMR'), ('ILS'),
    ('EGP'), ('MAD'), ('DZD'), ('TND'), ('NGN'), ('KES'), ('GHS'), ('ETB'), ('UGX'),
    ('TZS'), ('XOF'), ('XAF'), ('ARS'), ('CLP'), ('COP'), ('PEN'), ('UYU'), ('BOB'),
    ('PYG'), ('CRC'), ('DOP'), ('GTQ'), ('HNL'), ('NIO'), ('PAB'), ('JMD'), ('TTD'),
    ('BBD'), ('BSD'), ('XCD'), ('PKR'), ('BDT'), ('LKR'), ('NPR'), ('MMK'), ('THB'),
    ('VND'), ('MYR'), ('IDR'), ('PHP'), ('KHR'), ('LAK'), ('BND'), ('MOP'), ('KZT'),
    ('UZS'), ('GEL'), ('AMD'), ('AZN'), ('RSD'), ('UAH'), ('MDL'), ('ISK'), ('ALL'),
    ('MKD'), ('BAM'), ('JOD'), ('LBP'), ('IQD'), ('IRR'), ('AFN'), ('MUR'), ('BWP'),
    ('ZMW'), ('AOA'), ('MZN'), ('SCR')) AS v (code);

-- An instrument is a thing that can be held and priced: a security, an option,
-- a future, a currency. A price attaches to an instrument, and also to a
-- listing when its currency is known. A holding is of the instrument: a
-- security's listings are summed together.
--
-- A currency family is an instrument of class cash, named by a currency
-- identifier per code in the family. Its listing in its own family is money
-- in that currency.
--
-- FX rates are represented as listings on a currency instrument in another
-- currency.
CREATE TABLE instrs (
    id          uuid        PRIMARY KEY,
    asset_class asset_class NOT NULL REFERENCES asset_class_tree (class),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- A listing is one trading currency family of an instrument. Venues are
-- fungible within a listing: a composite identifier names the venues of one
-- market, tickers sharing a composite name one listing, and a listing carries
-- a composite per market in which it trades. A ticker is kept per venue as a
-- handle for a datasource that must be asked at one venue. A listing's
-- primary venue names it to the user.
CREATE TABLE listings (
    id            uuid        PRIMARY KEY,
    instr_id uuid        NOT NULL REFERENCES instrs (id),
    -- currency is a family code: a listing quoted in pence is the GBP listing.
    currency      text        NOT NULL,
    -- primary_mic is the operating MIC of the venue a datasource stated as
    -- the listing's primary; NULL where none has.
    primary_mic   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (instr_id, currency),
    UNIQUE (id, instr_id),
    FOREIGN KEY (currency, currency) REFERENCES currencies (code, family)
);

-- An identifier names an instrument or one of its listings by a type, a
-- domain and a value, the domain empty where its type has none. One triple
-- identifies one instrument or listing. The table holds identity: the stable
-- identifiers, the option symbols and the broker descriptions. Resolution may
-- match through every row. A ticker names different instruments on different
-- days, so the tickers table holds it, and its ticker validity names the
-- instrument for each day; see 010_ticker_validity.sql. An
-- option symbol matches through its row, because the system does not
-- normalise option symbols through the corporate actions of their underlying.
CREATE TABLE identifiers (
    id            uuid            PRIMARY KEY,
    instr_id uuid            NOT NULL REFERENCES instrs (id),
    -- listing_id is only set for a listing grain identifier, NULL otherwise.
    listing_id    uuid            REFERENCES listings (id),
    type          identifier_type NOT NULL,
    domain        text            NOT NULL DEFAULT '',
    value         text            NOT NULL,
    created_at    timestamptz     NOT NULL DEFAULT now(),
    grain         identifier_grain NOT NULL GENERATED ALWAYS AS
        (CASE WHEN listing_id IS NULL THEN 'instr'::identifier_grain
              ELSE 'listing'::identifier_grain END) STORED,
    FOREIGN KEY (type, grain) REFERENCES identifier_type_traits (type, grain),
    -- Deferrable so a merge can move a listing and its identifiers between
    -- instruments in separate statements of one transaction.
    FOREIGN KEY (listing_id, instr_id) REFERENCES listings (id, instr_id)
        DEFERRABLE INITIALLY IMMEDIATE,
    UNIQUE (type, domain, value),
    CHECK (type NOT IN ('mic_ticker', 'datasource_ticker'))
);

CREATE INDEX identifiers_instr_idx ON identifiers (instr_id);

WITH named AS (
    SELECT uuid_v7() AS id, code FROM currencies WHERE code = family
), cash AS (
    INSERT INTO instrs (id, asset_class)
    SELECT id, 'cash' FROM named
), cash_listings AS (
    INSERT INTO listings (id, instr_id, currency)
    SELECT uuid_v7(), id, code FROM named
)
INSERT INTO identifiers (id, instr_id, type, value)
SELECT uuid_v7(), named.id, 'currency', currencies.code
FROM named JOIN currencies ON currencies.family = named.code;

