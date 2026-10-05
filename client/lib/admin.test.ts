import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { UserRunSchema } from "@/gen/admin/v1/admin_pb";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { RunKind, RunSchema } from "@/gen/run/v1/run_pb";
import {
  filterQuery,
  flattenRuns,
  openFindingsBelow,
  queryHref,
  readFilters,
  runLabel,
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

  it("leaves an unset param out of an address", () => {
    expect(queryHref("/admin/blocks", { cleared: "1", before: "" })).toBe(
      "/admin/blocks?cleared=1",
    );
    expect(queryHref("/admin/blocks", { cleared: "" })).toBe("/admin/blocks");
    expect(filterQuery({ user: "u1" })).toBe("/admin/runs?user=u1");
  });

  it("names a run by its kind and when it started", () => {
    const run = create(RunSchema, {
      kind: RunKind.FETCH,
      createdAt: timestampFromDate(new Date("2026-09-24T10:00:00Z")),
    });
    expect(runLabel(run)).toBe("fetch run @ 2026-09-24 10:00 UTC");
    expect(runLabel(create(RunSchema, { kind: RunKind.FETCH }))).toBe(
      "fetch run",
    );
    expect(runLabel(undefined)).toBe("run");
  });

  it("walks a tree of runs and sums the findings below each", () => {
    const fetch = create(UserRunSchema, { userId: "f", openFindings: 2 });
    const resolution = create(UserRunSchema, {
      userId: "r",
      openFindings: 1,
      children: [fetch],
    });
    const statement = create(UserRunSchema, {
      userId: "s",
      children: [resolution],
    });
    expect(flattenRuns([statement]).map((r) => r.userId)).toEqual([
      "s",
      "r",
      "f",
    ]);
    expect(openFindingsBelow(statement)).toBe(3);
    expect(openFindingsBelow(fetch)).toBe(2);
  });
});
