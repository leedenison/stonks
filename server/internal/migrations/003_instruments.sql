-- +goose Up

-- An instrument is a thing that can be held and priced: a security, an option,
-- a future, cash. A listing is one currency an instrument trades in; an
-- instrument has zero or more, and none means its listings are not known. A
-- transaction, and later a price, attaches to an instrument and to a listing
-- when its currency is known.
--
-- An identifier names an instrument or one of its listings by a type, an
-- optional domain and a value. The identifier_type_traits table declares, per type,
-- its scope (who can recognise the value), what its domain is, its grain (the
-- kind of row it names) and its reassignment (whether the value moves between
-- instruments, and what the system relies on to know it has). A row of
-- identifiers attaches at its type's grain, which the generated grain column
-- and its foreign key enforce. One owner holds one triple (type, domain,
-- value) at most once, so an identifier names one subject.
--
-- A broker's description of an instrument is stated beside a currency, and the
-- two together are what the broker calls the line, so broker_description is
-- listing grain. Its domain is the broker and the channel the description
-- arrived through.
--
-- Ownership. An instrument, listing or identifier is owned by the user whose
-- statements alone support it, or by the system when owner_id is NULL. A system
-- owned parent may hold children of any owner; a user owned parent holds
-- children of that user only. owner_id is set at insert and never changes.
-- Both rules are enforced by triggers below.
--
-- Cash is one system owned instrument of class cash with a listing per row of
-- currencies, and on each listing a currency identifier whose value is the
-- code. A cash leg states that identifier and resolves to the listing without
-- creating anything; a cash holding is the sum of a user's quantities against
-- one listing. These are the only system owned rows.
--
-- A stated key is what one source states about an instrument: its
-- identifiers, asset class, currency and description. It is stored with the
-- statement that carried it, one row per distinct key, and is what resolution
-- answers. The identifiers it states become identifier rows only when
-- resolution admits them.
--
-- A transaction is a change in the quantity of one instrument held by a user,
-- with an order date and a settlement date, stated as at a date: the date on
-- which its values were true, and so which corporate events they reflect.
-- Replacement is keyed on user, broker and order date, so those are columns of
-- the row rather than reached through the statement.

CREATE TYPE broker AS ENUM ('ibkr', 'schwab', 'fidelity');

CREATE TYPE asset_class AS ENUM ('unknown', 'cash', 'security', 'equity', 'stock', 'etf',
    'mutual_fund', 'fixed_income', 'derivative', 'option', 'future', 'fx');

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
    ('future', 'derivative'),
    ('fx', 'security');

CREATE TYPE identifier_type AS ENUM ('isin', 'cusip', 'cins', 'wertpapier',
    'openfigi_share_class', 'sedol', 'openfigi_composite', 'mic_ticker', 'openfigi_ticker',
    'occ', 'currency', 'fx_pair', 'datasource_ticker', 'broker_id', 'broker_description');
CREATE TYPE identifier_scope AS ENUM ('registry', 'datasource', 'broker', 'source');
CREATE TYPE identifier_domain AS ENUM ('none', 'venue', 'datasource', 'broker', 'channel');
CREATE TYPE identifier_grain AS ENUM ('instrument', 'listing');
CREATE TYPE identifier_reassignment AS ENUM ('stable', 'mic_derived', 'unverifiable');

-- scope: 'registry' values are recognised by any source, 'datasource' and
-- 'broker' values only by the one that issued them, and 'source' values only
-- by the source and channel they came through.
-- domain: what qualifies the value. 'venue' is an ISO 10383 operating MIC.
-- reassignment: 'stable' values are assumed never reassigned, 'mic_derived'
-- values move exactly when the mic_ticker they derive from does, and
-- 'unverifiable' values are assumed never reassigned within their domain
-- because no datasource can witness a move.
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
    ('currency',             'registry',   'none',       'listing',    'stable'),
    ('fx_pair',              'registry',   'none',       'instrument', 'stable'),
    ('datasource_ticker',    'datasource', 'datasource', 'listing',    'mic_derived'),
    ('broker_id',            'broker',     'broker',     'instrument', 'stable'),
    ('broker_description',   'source',     'channel',    'listing',    'unverifiable');

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
    owner_id    uuid        REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE listings (
    id            uuid        PRIMARY KEY,
    instrument_id uuid        NOT NULL REFERENCES instruments (id),
    currency      text        NOT NULL REFERENCES currencies (code),
    owner_id      uuid        REFERENCES users (id),
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
    owner_id      uuid            REFERENCES users (id),
    created_at    timestamptz     NOT NULL DEFAULT now(),
    grain         identifier_grain NOT NULL GENERATED ALWAYS AS
        (CASE WHEN listing_id IS NULL THEN 'instrument'::identifier_grain
              ELSE 'listing'::identifier_grain END) STORED,
    FOREIGN KEY (type, grain) REFERENCES identifier_type_traits (type, grain),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id),
    UNIQUE NULLS NOT DISTINCT (owner_id, type, domain, value)
);

CREATE INDEX identifiers_instrument_idx ON identifiers (instrument_id);

-- +goose StatementBegin
CREATE FUNCTION check_owner_chain() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    parent_owner uuid;
BEGIN
    SELECT owner_id INTO parent_owner FROM instruments WHERE id = NEW.instrument_id;
    IF parent_owner IS NOT NULL AND NEW.owner_id IS DISTINCT FROM parent_owner THEN
        RAISE check_violation USING MESSAGE = format(
            '%s %s must be owned by the owner of instrument %s', TG_TABLE_NAME, NEW.id, NEW.instrument_id);
    END IF;
    IF TG_TABLE_NAME = 'identifiers' THEN
        IF NEW.listing_id IS NOT NULL THEN
            SELECT owner_id INTO parent_owner FROM listings WHERE id = NEW.listing_id;
            IF parent_owner IS NOT NULL AND NEW.owner_id IS DISTINCT FROM parent_owner THEN
                RAISE check_violation USING MESSAGE = format(
                    'identifier %s must be owned by the owner of listing %s', NEW.id, NEW.listing_id);
            END IF;
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION refuse_owner_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
        RAISE check_violation USING MESSAGE = format('%s %s: owner_id cannot change', TG_TABLE_NAME, OLD.id);
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER listings_owner_chain
    AFTER INSERT OR UPDATE OF instrument_id, owner_id ON listings
    FOR EACH ROW EXECUTE FUNCTION check_owner_chain();
CREATE CONSTRAINT TRIGGER identifiers_owner_chain
    AFTER INSERT OR UPDATE OF instrument_id, listing_id, owner_id ON identifiers
    FOR EACH ROW EXECUTE FUNCTION check_owner_chain();

CREATE TRIGGER instruments_owner_immutable BEFORE UPDATE OF owner_id ON instruments
    FOR EACH ROW EXECUTE FUNCTION refuse_owner_change();
CREATE TRIGGER listings_owner_immutable BEFORE UPDATE OF owner_id ON listings
    FOR EACH ROW EXECUTE FUNCTION refuse_owner_change();
CREATE TRIGGER identifiers_owner_immutable BEFORE UPDATE OF owner_id ON identifiers
    FOR EACH ROW EXECUTE FUNCTION refuse_owner_change();

WITH cash AS (
    INSERT INTO instruments (id, asset_class) VALUES (uuid_v7(), 'cash')
    RETURNING id
), cash_listings AS (
    INSERT INTO listings (id, instrument_id, currency)
    SELECT uuid_v7(), cash.id, currencies.code FROM cash, currencies
    RETURNING id, instrument_id, currency
)
INSERT INTO identifiers (id, instrument_id, listing_id, type, value)
SELECT uuid_v7(), instrument_id, id, 'currency', currency FROM cash_listings;

-- identifiers is a JSON array of objects with type, value and, when stated, a
-- domain, sorted by type, domain and value with no domain sorting first, so
-- that two statements of one key compare equal. The whole key is the unique
-- index, which bounds it at one btree entry, about 2.7KB.
--
-- currency is the code as the source stated it, checked against nothing.
CREATE TABLE stated_keys (
    id          uuid        PRIMARY KEY,
    statement_id   uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    asset_class asset_class REFERENCES asset_class_tree (class),
    currency    text,
    description text,
    identifiers jsonb       NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(identifiers) = 'array'),
    FOREIGN KEY (statement_id, user_id) REFERENCES runs (id, user_id),
    UNIQUE NULLS NOT DISTINCT (statement_id, asset_class, currency, description, identifiers)
);

-- listing_id and currency are set when the source stated the currency, and
-- currency is the precise code stated. quantity is in units of the instrument:
-- shares, contracts, or money for cash.
CREATE TABLE transactions (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id),
    broker          broker      NOT NULL,
    statement_id       uuid        NOT NULL,
    stated_key_id   uuid        NOT NULL REFERENCES stated_keys (id),
    instrument_id   uuid        NOT NULL REFERENCES instruments (id),
    listing_id      uuid        REFERENCES listings (id),
    order_date      date        NOT NULL,
    settlement_date date        NOT NULL,
    as_at           date        NOT NULL,
    quantity        numeric     NOT NULL,
    currency        text        REFERENCES currencies (code),
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (statement_id, user_id) REFERENCES runs (id, user_id),
    FOREIGN KEY (listing_id, instrument_id) REFERENCES listings (id, instrument_id)
);

CREATE INDEX transactions_period_idx ON transactions (user_id, broker, order_date);
CREATE INDEX transactions_instrument_idx ON transactions (user_id, instrument_id, listing_id);
