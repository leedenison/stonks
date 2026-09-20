import { describe, expect, it } from "vitest";
import {
  iso,
  maxDate,
  minDate,
  monthNumber,
  nextDay,
  prevDay,
  today,
} from "./date";

describe("date", () => {
  it("reads today in the browser's zone", () => {
    expect(today(new Date(2025, 0, 31, 23, 30))).toBe("2025-01-31");
    expect(today(new Date(2025, 11, 1, 0, 10))).toBe("2025-12-01");
  });

  it("pads the month and the day", () => {
    expect(iso("2025", "3", "7")).toBe("2025-03-07");
    expect(iso("2025", "12", "31")).toBe("2025-12-31");
  });

  it("maps an abbreviated month name and nothing else", () => {
    expect(monthNumber("Jan")).toBe("01");
    expect(monthNumber("Dec")).toBe("12");
    expect(monthNumber("jan")).toBeUndefined();
    expect(monthNumber("Sept")).toBeUndefined();
  });

  const steps: [string, string][] = [
    ["2025-01-30", "2025-01-31"],
    ["2025-01-31", "2025-02-01"],
    ["2025-04-30", "2025-05-01"],
    ["2025-12-31", "2026-01-01"],
    ["2024-02-28", "2024-02-29"],
    ["2024-02-29", "2024-03-01"],
    ["2023-02-28", "2023-03-01"],
    ["2100-02-28", "2100-03-01"],
    ["2000-02-28", "2000-02-29"],
  ];

  it("steps a date by one day across month, year and leap day ends", () => {
    for (const [day, next] of steps) {
      expect(nextDay(day), `nextDay(${day})`).toBe(next);
      expect(prevDay(next), `prevDay(${next})`).toBe(day);
      expect(prevDay(nextDay(day)), `round trip of ${day}`).toBe(day);
    }
  });

  it("orders dates across a year end", () => {
    expect(maxDate("2025-12-31", "2026-01-01")).toBe("2026-01-01");
    expect(minDate("2025-12-31", "2026-01-01")).toBe("2025-12-31");
    expect(maxDate("2025-02-01", "2025-02-01")).toBe("2025-02-01");
  });
});
