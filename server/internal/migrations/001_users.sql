-- +goose Up

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TYPE user_role AS ENUM ('user', 'admin');

-- A user is a person with an account, identified internally by a UUID and
-- externally by the Google account they signed in with.
CREATE TABLE users (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The stable identifier of the Google account; null until the account's
    -- first Google sign-in binds it.
    google_subject text        UNIQUE,
    email          text        NOT NULL,
    name           text        NOT NULL DEFAULT '',
    role           user_role   NOT NULL DEFAULT 'user',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));
