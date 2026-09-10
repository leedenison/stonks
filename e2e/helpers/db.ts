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

// deleteUser removes a user this suite created.
export async function deleteUser(id: string): Promise<void> {
  await db().query("DELETE FROM users WHERE id = $1", [id]);
}

export async function closeDB(): Promise<void> {
  await pool?.end();
  pool = null;
}
