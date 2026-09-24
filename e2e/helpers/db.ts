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

// seedUser creates a user with an invented identity that no other spec can
// collide with, and returns it.
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

// deleteUser removes a user this suite created and everything the user's
// runs wrote, in the order the foreign keys allow, as one transaction.
// Instruments, listings and identifiers are the system's and are never
// deleted by the suite.
export async function deleteUser(id: string): Promise<void> {
  const client = await db().connect();
  try {
    await client.query("BEGIN");
    for (const table of [
      "transactions",
      "resolution_keys",
      "statement_splits",
      "statement_items",
      "stated_keys",
      "statements",
    ]) {
      await client.query(`DELETE FROM ${table} WHERE user_id = $1`, [id]);
    }
    await client.query(
      "DELETE FROM findings WHERE run_id IN (SELECT id FROM runs WHERE user_id = $1)",
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
