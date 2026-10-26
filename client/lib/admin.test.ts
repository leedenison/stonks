import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { UserRunSchema } from "@/gen/admin/run/v1/run_pb";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { RunKind, RunSchema } from "@/gen/run/v1/run_pb";
import {
  filterQuery,
  flattenRuns,
  instrumentQuery,
  openFindingsBelow,
  queryHref,
  readFilters,
  readInstrumentList,
  readTickerParam,
  runLabel,
  runTitle,
  tickerParam,
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

  it("round trips the instruments address", () => {
    const p = readInstrumentList(
      new URLSearchParams("id=i1&tab=tickers&ticker=XNAS:BRK.B"),
    );
    expect(p).toEqual({
      after: "",
      id: "i1",
      q: "",
      class: "",
      tab: "tickers",
      ticker: "XNAS:BRK.B",
    });
    expect(instrumentQuery(p)).toBe(
      "/admin/instruments?id=i1&tab=tickers&ticker=XNAS%3ABRK.B",
    );
    expect(readTickerParam(p.ticker)).toEqual({
      at: "XNAS",
      value: "BRK.B",
    });
    expect(tickerParam("XNAS", "BRK.B")).toBe("XNAS:BRK.B");
    expect(readTickerParam("XNAS")).toBeUndefined();
    expect(readTickerParam(":ACME")).toBeUndefined();
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

  it("names a fetch run by the host it called", () => {
    const run = create(RunSchema, {
      kind: RunKind.FETCH,
      createdAt: timestampFromDate(new Date("2026-09-24T10:00:00Z")),
    });
    expect(
      runTitle(
        create(UserRunSchema, { run, endpoint: "http://massive.vcr:8080" }),
      ),
    ).toBe("massive.vcr:8080");
    expect(
      runTitle(create(UserRunSchema, { run, endpoint: "not a url" })),
    ).toBe("not a url");
    expect(runTitle(create(UserRunSchema, { run }))).toBe(
      "fetch run @ 2026-09-24 10:00 UTC",
    );
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
