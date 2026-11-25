-- +goose Up

-- A price coverage row is one period over which one datasource answered for
-- the end of day prices of one listing. The datasource reported every price
-- it holds for the period, so a day inside it without a price had no session.
-- The periods of one datasource for one listing never overlap: the first
-- answer for a day stands.
CREATE TABLE price_coverage (
    id             uuid PRIMARY KEY,
    listing_id     uuid NOT NULL REFERENCES listings (id) ON DELETE CASCADE,
    datasource     text NOT NULL REFERENCES datasources (name),
    fetch_key_id   uuid NOT NULL REFERENCES fetch_keys (id) ON DELETE CASCADE,
    covered_from   date NOT NULL,
    covered_before date NOT NULL,
    CHECK (covered_from < covered_before),
    EXCLUDE USING gist (listing_id WITH =, datasource WITH =,
                        daterange(covered_from, covered_before) WITH &&)
);

CREATE INDEX price_coverage_fetch_key_idx ON price_coverage (fetch_key_id);

-- A price is the end of day price of a listing on one day, as one datasource
-- served it. Its listing, datasource and fetch key are those of its coverage
-- row, whose period holds its day. The rate of a currency is a price of the
-- USD listing of that currency.
CREATE TABLE prices (
    coverage_id uuid    NOT NULL REFERENCES price_coverage (id) ON DELETE CASCADE,
    day         date    NOT NULL,
    -- price is in the family currency of the listing, whatever unit the
    -- datasource quoted.
    price       numeric NOT NULL CHECK (price > 0),
    -- as_at is the day on which the price was true of a share. A datasource
    -- that adjusts its history for splits states an as_at after the day of the
    -- price. A read restates the price from as_at to today.
    as_at       date    NOT NULL,
    PRIMARY KEY (coverage_id, day),
    CHECK (as_at >= day)
);
