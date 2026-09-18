import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { formatElapsed, formatInstant } from "./format";

describe("format", () => {
  it("renders an instant to the minute in UTC", () => {
    expect(
      formatInstant(timestampFromDate(new Date("2026-09-16T08:30:59Z"))),
    ).toBe("2026-09-16 08:30 UTC");
  });

  it("renders elapsed time in its two largest units", () => {
    expect(formatElapsed(0)).toBe("0s");
    expect(formatElapsed(4500)).toBe("4s");
    expect(formatElapsed(65000)).toBe("1m 05s");
    expect(formatElapsed(2 * 3600e3 + 10 * 60e3 + 3e3)).toBe("2h 10m");
    expect(formatElapsed(-5)).toBe("0s");
  });
});
