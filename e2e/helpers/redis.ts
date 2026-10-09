import Redis from "ioredis";
import { redisURL } from "./config";

let redis: Redis | null = null;

// store returns the worker's Redis client, opened on first use.
export function store(): Redis {
  redis ??= new Redis(redisURL);
  return redis;
}

export async function closeRedis(): Promise<void> {
  await redis?.quit();
  redis = null;
}
