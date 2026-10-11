import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { invalidateResolved } from "./query-keys";

describe("invalidateResolved", () => {
  const keys = async (statementIds?: string[]) => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    await invalidateResolved(client, statementIds);
    return invalidate.mock.calls.map((c) => c[0]?.queryKey);
  };

  it("refreshes the holdings, the transactions and every statement", async () => {
    expect(await keys()).toEqual([
      ["holdings"],
      ["transactions"],
      ["statements"],
    ]);
  });

  it("refreshes only the statements named", async () => {
    expect(await keys(["s1", "s2"])).toEqual([
      ["holdings"],
      ["transactions"],
      ["statements", "s1"],
      ["statements", "s2"],
    ]);
  });
});
