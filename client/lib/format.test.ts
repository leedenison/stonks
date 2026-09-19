import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { formatElapsed, formatInstant, formatQuantity } from "./format";

describe("format", () => {
  it("renders an instant to the minute in UTC", () => {
    expect(
      formatInstant(timestampFromDate(new Date("2026-09-16T08:30:59Z"))),
    ).toBe("2026-09-16 08:30 UTC");
  });

  it("rounds a quantity to two places and cuts a long one", () => {
    expect(formatQuantity("100")).toBe("100.00");
    expect(formatQuantity("-0.03")).toBe("-0.03");
    expect(formatQuantity("13587.849")).toBe("13587.85");
    expect(formatQuantity("1.005")).toBe("1.01");
    expect(formatQuantity("-0.001")).toBe("0.00");
    expect(formatQuantity("123456789.12")).toBe("12345678\u2026");
    expect(formatQuantity("1234567.1")).toBe("1234567\u2026");
    expect(() => formatQuantity("abc")).toThrow("not a decimal: abc");
  });

  it("renders elapsed time in its two largest units", () => {
    expect(formatElapsed(0)).toBe("0s");
    expect(formatElapsed(4500)).toBe("4s");
    expect(formatElapsed(65000)).toBe("1m 05s");
    expect(formatElapsed(2 * 3600e3 + 10 * 60e3 + 3e3)).toBe("2h 10m");
    expect(formatElapsed(-5)).toBe("0s");
  });
});
