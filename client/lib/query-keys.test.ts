import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import {
  invalidateAttested,
  invalidateCleared,
  invalidateDecided,
  invalidateResolved,
} from "./query-keys";

describe("invalidateResolved", () => {
  const keys = async (stmtIds?: string[]) => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    await invalidateResolved(client, stmtIds);
    return invalidate.mock.calls.map((c) => c[0]?.queryKey);
  };

  it("refreshes the holdings, the transactions and every statement", async () => {
    expect(await keys()).toEqual([["holdings"], ["txns"], ["stmts"]]);
  });

  it("refreshes only the statements named", async () => {
    expect(await keys(["s1", "s2"])).toEqual([
      ["holdings"],
      ["txns"],
      ["stmts", "s1"],
      ["stmts", "s2"],
    ]);
  });
});

describe("alerts", () => {
  it.each([
    ["cleared", invalidateCleared],
    ["decided", invalidateDecided],
    ["attested", invalidateAttested],
  ])("refreshes the alerts when something is %s", async (_, invalidate) => {
    const client = new QueryClient();
    const spy = vi.spyOn(client, "invalidateQueries");
    await invalidate(client);
    expect(spy.mock.calls.map((c) => c[0]?.queryKey)).toContainEqual([
      "alerts",
    ]);
  });
});
