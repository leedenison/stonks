// Correctness rules only; Prettier owns formatting.
import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import tseslint from "typescript-eslint";

export default defineConfig([
  globalIgnores(["gen/**", "test-results/**", "playwright-report/**"]),
  js.configs.recommended,
  ...tseslint.configs.recommended,
]);
