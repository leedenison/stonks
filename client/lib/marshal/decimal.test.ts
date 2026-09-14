import { describe, expect, it } from "vitest";
import { add, isZero, negate, normalise } from "./decimal";

describe("normalise", () => {
  it.each([
    ["1234.56", "1234.56"],
    ["-$20,000.00", "-20000"],
    ["$1,234.50", "1234.5"],
    ["+5", "5"],
    [".5", "0.5"],
    ["-0", "0"],
    ["0.00", "0"],
    ["1007.0", "1007"],
    ["007", "7"],
  ])("%s -> %s", (input, want) => {
    expect(normalise(input)).toBe(want);
  });

  it("rejects what is not a decimal", () => {
    expect(() => normalise("")).toThrow();
    expect(() => normalise("abc")).toThrow();
    expect(() => normalise("1e5")).toThrow();
  });
});

describe("add", () => {
  it("is exact across scales", () => {
    expect(add("-14848.84740216", "0.78710536")).toBe("-14848.0602968");
    expect(add("0.1", "0.2")).toBe("0.3");
    expect(add("1", "-1")).toBe("0");
    expect(add("$50,462.77", "$0.03")).toBe("50462.8");
  });
});

describe("negate", () => {
  it("flips the sign and keeps zero unsigned", () => {
    expect(negate("181")).toBe("-181");
    expect(negate("-0.5")).toBe("0.5");
    expect(negate("0")).toBe("0");
  });
});

describe("isZero", () => {
  it("reads any scale", () => {
    expect(isZero("0.00")).toBe(true);
    expect(isZero("-0")).toBe(true);
    expect(isZero("0.001")).toBe(false);
  });
});
