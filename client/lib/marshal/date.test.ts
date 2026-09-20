import { describe, expect, it } from "vitest";
import { nextDay, prevDay, today } from "./date";

describe("date", () => {
  it("reads today in the browser's zone", () => {
    expect(today(new Date(2025, 0, 31, 23, 30))).toBe("2025-01-31");
    expect(today(new Date(2025, 11, 1, 0, 10))).toBe("2025-12-01");
  });

  it("steps a date by one day across a month end", () => {
    expect(nextDay("2025-01-31")).toBe("2025-02-01");
    expect(prevDay("2025-03-01")).toBe("2025-02-28");
  });
});
