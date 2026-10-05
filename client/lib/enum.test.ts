import { describe, expect, it } from "vitest";
import { RunKind, RunState } from "@/gen/run/v1/run_pb";
import { enumLabel, enumParam, enumValues, fromParam } from "./enum";

describe("enum", () => {
  it("carries an enum value as its lower case name", () => {
    expect(enumParam(RunKind, RunKind.STATEMENT)).toBe("statement");
    expect(fromParam(RunKind, "statement")).toBe(RunKind.STATEMENT);
    expect(fromParam(RunKind, "unspecified")).toBeUndefined();
    expect(fromParam(RunKind, "nonsense")).toBeUndefined();
    expect(enumLabel(RunState, RunState.INTERRUPTED)).toBe("interrupted");
  });

  it("offers every value but UNSPECIFIED", () => {
    expect(enumValues(RunState)).not.toContain(RunState.UNSPECIFIED);
    expect(enumValues(RunState)).toContain(RunState.INTERRUPTED);
  });
});
