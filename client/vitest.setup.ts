import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Globals are off, so testing-library's own cleanup never registers.
afterEach(cleanup);
