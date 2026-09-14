-- +goose Up

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- A version 7 UUID: 48 bits of Unix milliseconds written over the random
-- bits of gen_random_uuid(), whose variant bits stay. Bits 52 and 53 turn the
-- version nibble from 0100 into 0111.
-- +goose StatementBegin
CREATE FUNCTION uuid_v7() RETURNS uuid LANGUAGE sql VOLATILE AS $$
    SELECT encode(
        set_bit(
            set_bit(
                overlay(uuid_send(gen_random_uuid())
                        PLACING substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
                        FROM 1 FOR 6),
                52, 1),
            53, 1),
        'hex')::uuid;
$$;
-- +goose StatementEnd

CREATE TYPE user_role AS ENUM ('user', 'admin');

-- A user is a person with an account, identified internally by a UUID and
-- externally by the Google account they signed in with.
CREATE TABLE users (
    id             uuid        PRIMARY KEY DEFAULT uuid_v7(),
    -- The stable identifier of the Google account; null until the account's
    -- first Google sign-in binds it.
    google_subject text        UNIQUE,
    email          text        NOT NULL,
    name           text        NOT NULL DEFAULT '',
    role           user_role   NOT NULL DEFAULT 'user',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));
