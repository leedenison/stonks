// Correctness rules only; Prettier owns formatting.
import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

export default defineConfig([
  globalIgnores([".next/**", "gen/**", "next-env.d.ts"]),
  ...nextVitals,
  ...nextTs,
]);
