import { randomUUID } from "node:crypto";
import type { JsonObject } from "@bufbuild/protobuf";
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
  config = {},
}: {
  name: string;
  endpoint: string;
  credential: string | null;
  enabled?: boolean;
  precedence?: number;
  config?: JsonObject;
}): Promise<void> {
  await db().query(
    `INSERT INTO datasources (name, enabled, precedence, credential, endpoint, config)
     VALUES ($1, $2, $3, $4, $5, $6)
     ON CONFLICT (name) DO UPDATE
     SET enabled = EXCLUDED.enabled, precedence = EXCLUDED.precedence,
         credential = EXCLUDED.credential, endpoint = EXCLUDED.endpoint,
         config = EXCLUDED.config`,
    [name, enabled, precedence, credential, endpoint, JSON.stringify(config)],
  );
}

// setIdentifierEventSource makes datasource the identifier event source of
// market. No RPC sets it.
export async function setIdentifierEventSource(
  market: string,
  datasource: string,
): Promise<void> {
  await db().query(
    "UPDATE markets SET identifier_event_source = $2 WHERE code = $1",
    [market, datasource],
  );
}

// setCorpActSource makes datasource the corporate actions source of market.
// Only a database update sets it.
export async function setCorpActSource(
  market: string,
  datasource: string,
): Promise<void> {
  await db().query("UPDATE markets SET corp_act_source = $2 WHERE code = $1", [
    market,
    datasource,
  ]);
}

// deleteDatasource removes a row this suite seeded. No fetch names that
// datasource, so nothing references the row.
export async function deleteDatasource(name: string): Promise<void> {
  await db().query("DELETE FROM datasources WHERE name = $1", [name]);
}

// deleteUsers removes users this suite created and everything their runs wrote,
// as one transaction. The schema cascades from a user to their rows.
// Instruments, listings and identifiers are the system's and are never deleted
// by the suite; those the users' fetches created lose their provenance and
// stand as reference data to later specs. An administrator's replay of another
// user's run names both, so it goes before either user. An administrator's
// attest identifier run is a system run that names them, so it goes with its
// assertions before the administrator. The API offers no way to delete a user.
export async function deleteUsers(ids: string[]): Promise<void> {
  const client = await db().connect();
  const keys = "SELECT id FROM fetch_keys WHERE user_id = ANY($1)";
  try {
    await client.query("BEGIN");
    for (const table of ["instrs", "listings", "identifiers"]) {
      await client.query(
        `UPDATE ${table} SET fetch_key_id = NULL WHERE fetch_key_id IN (${keys})`,
        [ids],
      );
    }
    await client.query("DELETE FROM replays WHERE started_by = ANY($1)", [ids]);
    await client.query(
      "DELETE FROM runs WHERE id IN (SELECT run_id FROM identifier_assertions WHERE attested_by = ANY($1))",
      [ids],
    );
    await client.query("DELETE FROM users WHERE id = ANY($1)", [ids]);
    await client.query("COMMIT");
  } catch (e) {
    await client.query("ROLLBACK");
    throw e;
  } finally {
    client.release();
  }
}

// deleteRuns removes system runs and everything beneath them. Withdrawing an
// identifier attestation removes the only assertion that names the
// administrator, so deleteUsers cannot find the run. No RPC deletes a run.
export async function deleteRuns(ids: string[]): Promise<void> {
  await db().query("DELETE FROM runs WHERE id = ANY($1)", [ids]);
}

export async function closeDB(): Promise<void> {
  await pool?.end();
  pool = null;
}

// described is the instrument that seedDescribedInstr seeds, for
// description.spec.ts. The ZZ prefix keeps the description and ISIN unique.
export const described = {
  broker: "fidelity_uk",
  description: "ZZ DESCRIBED GLOBAL EQUITY FUND ACC",
  isin: "ZZ0000000091",
  ticker: "ZZDESC",
};

// seedDescribedInstr seeds the facts of described as reference data. A
// product API cannot write a description, so SQL is the only route. A
// rebuild derives the ticker's validity and the name of the holding from the
// facts.
export async function seedDescribedInstr(): Promise<void> {
  const { broker, description, isin, ticker } = described;
  const client = await db().connect();
  try {
    await client.query("BEGIN");
    const instr = await client.query<{ id: string }>(
      "INSERT INTO instrs (id, asset_class) VALUES (uuid_v7(), 'equity') RETURNING id",
    );
    const id = instr.rows[0].id;
    await client.query(
      "INSERT INTO listings (id, instr_id, currency) VALUES (uuid_v7(), $1, 'GBP')",
      [id],
    );
    await client.query(
      "INSERT INTO identifiers (id, instr_id, type, value) VALUES (uuid_v7(), $1, 'isin', $2)",
      [id, isin],
    );
    await client.query(
      "INSERT INTO identifiers (id, instr_id, type, domain, value) VALUES (uuid_v7(), $1, 'broker_description', $2, $3)",
      [id, broker, description],
    );
    const tickerRow = await client.query<{ id: string }>(
      "INSERT INTO tickers (id, market, value) VALUES (uuid_v7(), 'XLON', $1) RETURNING id",
      [ticker],
    );
    const fetch = await client.query<{ id: string }>(
      "INSERT INTO runs (id, kind, trigger, state) VALUES (uuid_v7(), 'datasource_fetch', 'administrator', 'completed') RETURNING id",
    );
    const fetchId = fetch.rows[0].id;
    await client.query(
      "INSERT INTO fetches (id, datasource, endpoint, kind) VALUES ($1, 'eodhd', 'http://eodhd.vcr', 'identity')",
      [fetchId],
    );
    const key = await client.query<{ id: string }>(
      "INSERT INTO fetch_keys (id, fetch_id, outcome, attempts, sent_type, sent_value, answered_at) VALUES (uuid_v7(), $1, 'served', 1, 'isin', $2, '2025-01-02') RETURNING id",
      [fetchId, isin],
    );
    await client.query(
      "INSERT INTO identifier_assertions (id, fetch_key_id, ticker_id, market, instr_id, asserted_from, asserted_before, venue, currency) VALUES (uuid_v7(), $1, $2, 'XLON', $3, '2025-01-02', '2025-01-03', 'XLON', 'GBP')",
      [key.rows[0].id, tickerRow.rows[0].id, id],
    );
    await client.query("COMMIT");
  } catch (e) {
    await client.query("ROLLBACK");
    throw e;
  } finally {
    client.release();
  }
}
