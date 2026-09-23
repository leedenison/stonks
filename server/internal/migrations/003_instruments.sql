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

CREATE TYPE identifier_type AS ENUM ('isin', 'cusip', 'cins', 'wertpapier',
    'openfigi_share_class', 'sedol', 'openfigi_composite', 'mic_ticker', 'openfigi_ticker',
    'occ', 'currency', 'datasource_ticker', 'broker_id');

-- What is used to qualify the identifier value.
-- 'global' identifier values are not partitioned (their domain is NULL).  They
-- are recognised by all parties.
-- 'venue' identifier values are partitioned by ISO 10383 operating MIC. They
-- are recognised by all parties.
-- 'issuer' identifier values are partitioned by their issuer (ie. datasource
-- or broker).  'issuer' identifier values are recognised only by the issuer.
CREATE TYPE identifier_domain AS ENUM ('global', 'venue', 'issuer');
CREATE TYPE identifier_grain AS ENUM ('instrument', 'listing');

-- How readily an identifier is reassigned.
-- 'stable' identifiers are assumed to never be reassigned.
-- 'mic_derived' identifiers are reassigned when the mic_ticker they derive
-- from is reassigned.
CREATE TYPE identifier_reassignment AS ENUM ('stable', 'mic_derived');

CREATE TABLE identifier_type_traits (
    type         identifier_type         PRIMARY KEY,
    domain       identifier_domain       NOT NULL,
    grain        identifier_grain        NOT NULL,
    reassignment identifier_reassignment NOT NULL,
    UNIQUE (type, grain)
);

INSERT INTO identifier_type_traits (type, domain, grain, reassignment) VALUES
    ('isin',                 'global', 'instrument', 'stable'),
    ('cusip',                'global', 'instrument', 'stable'),
    ('cins',                 'global', 'instrument', 'stable'),
    ('wertpapier',           'global', 'instrument', 'stable'),
    ('openfigi_share_class', 'global', 'instrument', 'stable'),
    ('sedol',                'global', 'listing',    'stable'),
    ('openfigi_composite',   'global', 'listing',    'stable'),
    ('mic_ticker',           'venue',  'listing',    'mic_derived'),
    ('openfigi_ticker',      'venue',  'listing',    'mic_derived'),
    ('occ',                  'global', 'instrument', 'mic_derived'),
    ('currency',             'global', 'instrument', 'stable'),
    ('datasource_ticker',    'issuer', 'listing',    'mic_derived'),
    ('broker_id',            'issuer', 'instrument', 'stable');

-- The currency codes a listing may be quoted in: ISO 4217, plus GBX for
-- sterling in pence, which is a listing of its own beside GBP.
CREATE TABLE currencies (
    code text PRIMARY KEY
);

INSERT INTO currencies (code) VALUES
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
    ('ZMW'), ('AOA'), ('MZN'), ('SCR');

-- An instrument is a thing that can be held and priced: a security, an option,
-- a future, a currency. A price attaches to an instrument, and also to a
-- listing when its currency is known. A holding is of the instrument: a
-- security's listings are summed together.
--
-- A currency is an instrument of class cash, named by a currency
-- identifier whose value is its code. Its listing in its own currency is
-- money in that currency.
--
-- FX rates are represented as listings on a currency instrument in another
-- currency.
CREATE TABLE instruments (
    id          uuid        PRIMARY KEY,
    asset_class asset_class NOT NULL REFERENCES asset_class_tree (class),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- A listing is one currency an instrument trades in.  Since neither brokers
-- nor price sources distinguish venues reliably, they are treated as fungible.
CREATE TABLE listings (
    id            uuid        PRIMARY KEY,
    instrument_id uuid        NOT NULL REFERENCES instruments (id),
    currency      text        NOT NULL REFERENCES currencies (code),
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (instrument_id, currency),
    UNIQUE (id, instrument_id)
);

-- An identifier names an instrument or one of its listings by a type, an
-- optional domain and a value. An identifier names one subject.
CREATE TABLE identifiers (
    id            uuid            PRIMARY KEY,
    instrument_id uuid            NOT NULL REFERENCES instruments (id),
    -- listing_id is only set for a listing grain identifier, NULL otherwise.
    listing_id    uuid            REFERENCES listings (id),
    type          identifier_type NOT NULL,
    domain        text,
    value         text            NOT NULL,
    created_at    timestamptz     NOT NULL DEFAULT now(),
    grain         identifier_grain NOT NULL GENERATED ALWAYS AS
        (CASE WHEN listing_id IS NULL THEN 'instrument'::identifier_grain
              ELSE 'listing'::identifier_grain END) STORED,
    FOREIGN KEY (type, grain) REFERENCES identifier_type_traits (type, grain),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id),
    UNIQUE NULLS NOT DISTINCT (type, domain, value)
);

CREATE INDEX identifiers_instrument_idx ON identifiers (instrument_id);

WITH named AS (
    SELECT uuid_v7() AS id, code FROM currencies
), cash AS (
    INSERT INTO instruments (id, asset_class)
    SELECT id, 'cash' FROM named
), cash_listings AS (
    INSERT INTO listings (id, instrument_id, currency)
    SELECT uuid_v7(), id, code FROM named
)
INSERT INTO identifiers (id, instrument_id, type, value)
SELECT uuid_v7(), id, 'currency', code FROM named;

