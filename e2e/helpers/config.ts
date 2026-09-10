// Where the suite finds the stack. The playwright service sets every E2E_*
// variable; the fallbacks are the e2e stack's published ports, for a run
// from the host.
export const baseURL = process.env.E2E_BASE_URL ?? "http://localhost:8081";
export const databaseURL =
  process.env.E2E_DATABASE_URL ??
  "postgres://stonks:stonks@localhost:5434/stonks?sslmode=disable";
export const redisURL = process.env.E2E_REDIS_URL ?? "redis://localhost:6381/0";
