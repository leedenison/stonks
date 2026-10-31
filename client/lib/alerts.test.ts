import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  AlertKind,
  AlertSchema,
  GetAlertDigestResponseSchema,
  AlertGroupSchema,
  TickerNamesNothingSchema,
  TickerValidityGapSchema,
} from "@/gen/admin/alert/v1/alert_pb";
import { BlockSchema } from "@/gen/admin/block/v1/block_pb";
import { GapSchema } from "@/gen/admin/coverage/v1/coverage_pb";
import { FindingKind, FindingSchema } from "@/gen/admin/run/v1/run_pb";
import { MarketTickerSchema, PeriodSchema } from "@/gen/type/v1/type_pb";
import { userRun } from "./test-utils";
import { alertRow, kindOrder, openCount, readAlertParams } from "./alerts";

const ticker = create(MarketTickerSchema, { market: "XNYS", value: "WMT" });

describe("alertRow", () => {
  it("opens a failed run", () => {
    const r = alertRow(
      create(AlertSchema, {
        kind: AlertKind.FAILED_RUN,
        subject: { case: "run", value: userRun({ id: "r1" }) },
      }),
    );
    expect(r.key).toBe("r1");
    expect(r.action).toEqual({ case: "open-run", runId: "r1" });
  });

  it("clears a block", () => {
    const r = alertRow(
      create(AlertSchema, {
        kind: AlertKind.BLOCK,
        subject: {
          case: "block",
          value: create(BlockSchema, { id: "b1", reason: "quota" }),
        },
      }),
    );
    expect(r.action).toEqual({ case: "clear-block", id: "b1" });
    expect(r.detail).toBe("quota");
  });

  it("attests a ticker that names nothing from its first transaction day", () => {
    const r = alertRow(
      create(AlertSchema, {
        kind: AlertKind.TICKER_NAMES_NOTHING,
        subject: {
          case: "namesNothing",
          value: create(TickerNamesNothingSchema, {
            ticker,
            keys: 2,
            txns: 9,
            firstTxnDay: "2019-03-04",
          }),
        },
      }),
    );
    expect(r.key).toBe("XNYS-WMT");
    expect([r.txns, r.keys, r.since]).toEqual([9, 2, "2019-03-04"]);
    expect(r.action).toEqual({
      case: "attest",
      ticker,
      from: "2019-03-04",
    });
  });

  it("attests a gap over its days and keys it by its start", () => {
    const r = alertRow(
      create(AlertSchema, {
        kind: AlertKind.TICKER_VALIDITY_GAP,
        subject: {
          case: "validityGap",
          value: create(TickerValidityGapSchema, {
            ticker,
            gap: create(GapSchema, {
              period: create(PeriodSchema, {
                from: "2003-01-01",
                before: "2003-09-10",
              }),
              keys: 1,
              txns: 4,
            }),
          }),
        },
      }),
    );
    expect(r.key).toBe("XNYS-WMT-2003-01-01");
    expect(r.action).toEqual({
      case: "attest",
      ticker,
      from: "2003-01-01",
      to: "2003-09-09",
    });
  });

  it("decides a corporate action and clears any other finding", () => {
    const finding = (corpActId?: string) =>
      create(FindingSchema, {
        id: "f1",
        kind: FindingKind.UNHANDLED_CORP_ACT,
        corpActId,
      });
    expect(
      alertRow(
        create(AlertSchema, {
          kind: AlertKind.CORP_ACT,
          subject: { case: "finding", value: finding("c1") },
        }),
      ).action,
    ).toEqual({ case: "decide", corpActId: "c1" });
    expect(
      alertRow(
        create(AlertSchema, {
          kind: AlertKind.FINDING,
          subject: { case: "finding", value: finding() },
        }),
      ).action,
    ).toEqual({ case: "clear-finding", id: "f1" });
  });
});

describe("readAlertParams", () => {
  it("reads the kind and the page token", () => {
    expect(
      readAlertParams(new URLSearchParams("kind=ticker_validity_gap&before=t")),
    ).toEqual({ kind: AlertKind.TICKER_VALIDITY_GAP, before: "t" });
  });

  it("reads an empty address as the digest", () => {
    expect(readAlertParams(new URLSearchParams())).toEqual({
      kind: undefined,
      before: "",
    });
  });
});

describe("openCount", () => {
  it("sums the groups", () => {
    const d = create(GetAlertDigestResponseSchema, {
      groups: kindOrder.map((kind, i) =>
        create(AlertGroupSchema, { kind, count: i }),
      ),
    });
    expect(openCount(d)).toBe(15);
    expect(openCount(undefined)).toBe(0);
  });
});
