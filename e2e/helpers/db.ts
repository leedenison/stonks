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

// seedDatasource inserts a datasources row, or leaves the one already there.
// No RPC creates a row.
export async function seedDatasource(
  name: string,
  endpoint: string,
  credential: string,
): Promise<void> {
  await db().query(
    "INSERT INTO datasources (name, enabled, precedence, credential, endpoint) VALUES ($1, true, 1, $2, $3) ON CONFLICT DO NOTHING",
    [name, credential, endpoint],
  );
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
