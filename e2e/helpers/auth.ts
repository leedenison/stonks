import { randomUUID } from "node:crypto";
import type { BrowserContext } from "@playwright/test";
import Redis from "ioredis";
import { baseURL, redisURL } from "./config";
import type { SeededUser } from "./db";

// The session record and its key are the contract the server's session
// package states; the cookie name is the one its service boundary reads.
const prefix = "stonks:session:";
const cookieName = "stonks_session";
const lifeSeconds = 7 * 24 * 60 * 60;

let redis: Redis | null = null;

function store(): Redis {
  redis ??= new Redis(redisURL);
  return redis;
}

// seedSession starts a session for user without going through Google, and
// returns its identifier, the cookie value.
export async function seedSession(user: SeededUser): Promise<string> {
  const id = randomUUID();
  const now = new Date();
  const record = {
    user_id: user.id,
    created_at: now.toISOString(),
    expires_at: new Date(now.getTime() + lifeSeconds * 1000).toISOString(),
  };
  await store().set(prefix + id, JSON.stringify(record), "EX", lifeSeconds);
  return id;
}

// deleteSession ends a session as the server would on expiry.
export async function deleteSession(id: string): Promise<void> {
  await store().del(prefix + id);
}

// injectSession gives the browser context the session cookie. The stack is
// reached over plain HTTP, so the cookie is not Secure.
export async function injectSession(
  context: BrowserContext,
  id: string,
): Promise<void> {
  await context.addCookies([
    {
      name: cookieName,
      value: id,
      url: baseURL,
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
}

// sessionCookie is the Cookie header carrying a session, for a request made
// outside the browser.
export function sessionCookie(id: string): string {
  return `${cookieName}=${id}`;
}

export async function closeRedis(): Promise<void> {
  await redis?.quit();
  redis = null;
}
