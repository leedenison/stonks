import { randomUUID } from "node:crypto";
import { Pool } from "pg";
import { databaseURL } from "./config";

export type Role = "user" | "admin";

export type SeededUser = {
  id: string;
  email: string;
  name: string;
  role: Role;
};

let pool: Pool | null = null;

function db(): Pool {
  pool ??= new Pool({ connectionString: databaseURL });
  return pool;
}

// seedUser creates a user with an invented identity distinct from every
// other spec's, and returns it.
export async function seedUser(role: Role = "user"): Promise<SeededUser> {
  const tag = randomUUID();
  const email = `e2e-${tag}@example.com`;
  const name = "E2E User";
  const { rows } = await db().query<{ id: string }>(
    "INSERT INTO users (google_subject, email, name, role) VALUES ($1, $2, $3, $4) RETURNING id",
    [`e2e-${tag}`, email, name, role],
  );
  return { id: rows[0].id, email, name, role };
}

// seedDatasource writes a datasources row as given, replacing any row with
// the same name. No RPC creates a row.
export async function seedDatasource({
  name,
  endpoint,
  credential,
  enabled = true,
  precedence = 1,
}: {
  name: string;
  endpoint: string;
  credential: string | null;
  enabled?: boolean;
  precedence?: number;
}): Promise<void> {
  await db().query(
    `INSERT INTO datasources (name, enabled, precedence, credential, endpoint)
     VALUES ($1, $2, $3, $4, $5)
     ON CONFLICT (name) DO UPDATE
     SET enabled = EXCLUDED.enabled, precedence = EXCLUDED.precedence,
         credential = EXCLUDED.credential, endpoint = EXCLUDED.endpoint`,
    [name, enabled, precedence, credential, endpoint],
  );
}

// deleteDatasource removes a row this suite seeded. No fetch names that
// datasource, so nothing references the row.
export async function deleteDatasource(name: string): Promise<void> {
  await db().query("DELETE FROM datasources WHERE name = $1", [name]);
}

// deleteUser removes a user this suite created and everything the user's
// runs wrote, in the order the foreign keys allow, as one transaction.
// Instruments, listings and identifiers are the system's and are never
// deleted by the suite; those the user's fetches created lose their
// provenance and stand as reference data to later specs.
export async function deleteUser(id: string): Promise<void> {
  const client = await db().connect();
  const keys = "SELECT id FROM fetch_keys WHERE user_id = $1";
  try {
    await client.query("BEGIN");
    await client.query(
      "DELETE FROM findings WHERE run_id IN (SELECT id FROM runs WHERE user_id = $1)",
      [id],
    );
    for (const table of [
      "datasource_blocks",
      "identity_coverage",
      "fetch_identifiers",
    ]) {
      await client.query(
        `DELETE FROM ${table} WHERE fetch_key_id IN (${keys})`,
        [id],
      );
    }
    for (const table of ["instruments", "listings", "identifiers"]) {
      await client.query(
        `UPDATE ${table} SET fetch_key_id = NULL WHERE fetch_key_id IN (${keys})`,
        [id],
      );
    }
    for (const table of [
      "fetch_keys",
      "fetches",
      "transactions",
      "resolution_keys",
      "statement_splits",
      "statement_items",
      "stated_keys",
      "statements",
    ]) {
      await client.query(`DELETE FROM ${table} WHERE user_id = $1`, [id]);
    }
    // An administrator's replay of another user's run names both, and the
    // two are deleted in no fixed order.
    await client.query(
      "DELETE FROM replays WHERE user_id = $1 OR started_by = $1",
      [id],
    );
    await client.query("DELETE FROM runs WHERE user_id = $1", [id]);
    await client.query("DELETE FROM users WHERE id = $1", [id]);
    await client.query("COMMIT");
  } catch (e) {
    await client.query("ROLLBACK");
    throw e;
  } finally {
    client.release();
  }
}

export async function closeDB(): Promise<void> {
  await pool?.end();
  pool = null;
}

// seedDescribedInstrument seeds reference data, an instrument with a GBP
// listing, an ISIN, an XLON ticker and a broker description, and returns its
// id. No product API writes a description, so SQL is the only route. System
// rows outlive a test and workers run in parallel, so a lock on the ISIN
// makes concurrent seeding find one instrument.
export async function seedDescribedInstrument(
  broker: string,
  description: string,
  isin: string,
  ticker: string,
): Promise<string> {
  const client = await db().connect();
  try {
    await client.query("BEGIN");
    await client.query("SELECT pg_advisory_xact_lock(hashtext($1))", [isin]);
    const found = await client.query<{ instrument_id: string }>(
      "SELECT instrument_id FROM identifiers WHERE type = 'isin' AND domain = '' AND value = $1",
      [isin],
    );
    if (found.rows.length > 0) {
      await client.query("COMMIT");
      return found.rows[0].instrument_id;
    }
    const instrument = await client.query<{ id: string }>(
      "INSERT INTO instruments (id, asset_class) VALUES (uuid_v7(), 'equity') RETURNING id",
    );
    const id = instrument.rows[0].id;
    const listing = await client.query<{ id: string }>(
      "INSERT INTO listings (id, instrument_id, currency) VALUES (uuid_v7(), $1, 'GBP') RETURNING id",
      [id],
    );
    await client.query(
      "INSERT INTO identifiers (id, instrument_id, type, value) VALUES (uuid_v7(), $1, 'isin', $2)",
      [id, isin],
    );
    await client.query(
      "INSERT INTO identifiers (id, instrument_id, listing_id, type, domain, value) VALUES (uuid_v7(), $1, $2, 'mic_ticker', 'XLON', $3)",
      [id, listing.rows[0].id, ticker],
    );
    await client.query(
      "INSERT INTO identifiers (id, instrument_id, type, domain, value) VALUES (uuid_v7(), $1, 'broker_description', $2, $3)",
      [id, broker, description],
    );
    await client.query("COMMIT");
    return id;
  } catch (e) {
    await client.query("ROLLBACK");
    throw e;
  } finally {
    client.release();
  }
}
