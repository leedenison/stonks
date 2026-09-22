-- +goose Up

-- An instrument is a thing that can be held and priced: a security, an option,
-- a future, a currency. A listing is one currency an instrument trades in,
-- and every venue quoting it in that currency is that one listing, since
-- neither brokers nor price sources tell venues apart reliably; an
-- instrument has zero or more, and none means its listings are not known. A
-- price attaches to an instrument and to a listing when its currency is
-- known. A holding is of the instrument: a security's
-- listings are summed into it and never converted between, since moving
-- between them costs fees and a spread, while a currency's listings are the
-- same money and it may be shown against any of them.
--
-- An identifier names an instrument or one of its listings by a type, an
-- optional domain and a value. The identifier_type_traits table declares, per type,
-- its scope (who can recognise the value), what its domain is, its grain (the
-- kind of row it names) and its reassignment (whether the value moves between
-- instruments, and what the system relies on to know it has). A row of
-- identifiers attaches at its type's grain, which the generated grain column
-- and its foreign key enforce. One triple (type, domain, value) exists at
-- most once, so an identifier names one subject.
--
-- A currency is an instrument of class cash, named by a currency
-- identifier whose value is its code. Its listing in its own currency is
-- money in it: a cash leg states the identifier and the currency, resolves
-- to that listing as any key resolves to a listing of the instrument its
-- identifier names, and a cash holding is the sum of a user's quantities
-- against the instrument. A listing in another currency carries the rate
-- between the two, and is made when a rate is first fetched rather than
-- seeded.

CREATE TYPE asset_class AS ENUM ('unknown', 'cash', 'security', 'equity', 'stock', 'etf',
    'mutual_fund', 'fixed_income', 'derivative', 'option', 'future');

-- The asset class tree. A leaf is a concrete class and a parent is the set of
-- the leaves below it, so a source states the narrowest class it can defend
-- and an internal node is a legal stored value. unknown is the root, the class
-- that rules nothing out.
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
CREATE TYPE identifier_scope AS ENUM ('registry', 'datasource', 'broker');
CREATE TYPE identifier_domain AS ENUM ('none', 'venue', 'datasource', 'broker');
CREATE TYPE identifier_grain AS ENUM ('instrument', 'listing');
CREATE TYPE identifier_reassignment AS ENUM ('stable', 'mic_derived');

-- scope: 'registry' values are recognised by any source, and 'datasource'
-- and 'broker' values only by the one that issued them.
-- domain: what qualifies the value. 'venue' is an ISO 10383 operating MIC.
-- reassignment: 'stable' values are assumed never reassigned, and
-- 'mic_derived' values move exactly when the mic_ticker they derive from
-- does.
CREATE TABLE identifier_type_traits (
    type         identifier_type         PRIMARY KEY,
    scope        identifier_scope        NOT NULL,
    domain       identifier_domain       NOT NULL,
    grain        identifier_grain        NOT NULL,
    reassignment identifier_reassignment NOT NULL,
    UNIQUE (type, grain)
);

INSERT INTO identifier_type_traits (type, scope, domain, grain, reassignment) VALUES
    ('isin',                 'registry',   'none',       'instrument', 'stable'),
    ('cusip',                'registry',   'none',       'instrument', 'stable'),
    ('cins',                 'registry',   'none',       'instrument', 'stable'),
    ('wertpapier',           'registry',   'none',       'instrument', 'stable'),
    ('openfigi_share_class', 'registry',   'none',       'instrument', 'stable'),
    ('sedol',                'registry',   'none',       'listing',    'stable'),
    ('openfigi_composite',   'registry',   'none',       'listing',    'stable'),
    ('mic_ticker',           'registry',   'venue',      'listing',    'mic_derived'),
    ('openfigi_ticker',      'registry',   'venue',      'listing',    'mic_derived'),
    ('occ',                  'registry',   'none',       'instrument', 'mic_derived'),
    ('currency',             'registry',   'none',       'instrument', 'stable'),
    ('datasource_ticker',    'datasource', 'datasource', 'listing',    'mic_derived'),
    ('broker_id',            'broker',     'broker',     'instrument', 'stable');

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

CREATE TABLE instruments (
    id          uuid        PRIMARY KEY,
    asset_class asset_class NOT NULL REFERENCES asset_class_tree (class),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE listings (
    id            uuid        PRIMARY KEY,
    instrument_id uuid        NOT NULL REFERENCES instruments (id),
    currency      text        NOT NULL REFERENCES currencies (code),
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (instrument_id, currency),
    UNIQUE (id, instrument_id)
);

-- listing_id is set for a row of listing grain and NULL for one of instrument
-- grain; instrument_id is always the instrument named or the listing's. The
-- foreign key on (type, grain) may carry no referential action, since grain is
-- generated.
CREATE TABLE identifiers (
    id            uuid            PRIMARY KEY,
    instrument_id uuid            NOT NULL REFERENCES instruments (id),
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

