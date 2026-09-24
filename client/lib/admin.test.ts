import { describe, expect, it } from "vitest";
import { RunKind, RunState } from "@/gen/run/v1/run_pb";
import {
  enumLabel,
  enumParam,
  filterQuery,
  fromParam,
  readFilters,
} from "./admin";

describe("admin", () => {
  it("round trips the filters through the address", () => {
    const f = readFilters(new URLSearchParams("state=failed&before=r9"));
    expect(f).toEqual({
      kind: "",
      trigger: "",
      state: "failed",
      user: "",
      before: "r9",
    });
    expect(filterQuery(f)).toBe("/admin/runs?state=failed&before=r9");
    expect(filterQuery({ ...f, state: "", before: "" })).toBe("/admin/runs");
  });

  it("carries an enum value as its lower case name", () => {
    expect(enumParam(RunKind, RunKind.STATEMENT)).toBe("statement");
    expect(fromParam(RunKind, "statement")).toBe(RunKind.STATEMENT);
    expect(fromParam(RunKind, "unspecified")).toBeUndefined();
    expect(fromParam(RunKind, "nonsense")).toBeUndefined();
    expect(enumLabel(RunState, RunState.INTERRUPTED)).toBe("interrupted");
  });
});
