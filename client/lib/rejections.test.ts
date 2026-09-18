import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { StatementItemSchema } from "@/gen/statement/v1/statement_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { groupByReason, keyLabel } from "./rejections";

const item = (ordinal: number, reason: string) =>
  create(StatementItemSchema, { ordinal, reason });

describe("groupByReason", () => {
  it("gathers rows under their reason in first-seen order", () => {
    const groups = groupByReason([
      item(3, "no key"),
      item(1, "order date outside the claimed period"),
      item(7, "no key"),
    ]);
    expect(groups.map((g) => g.reason)).toEqual([
      "no key",
      "order date outside the claimed period",
    ]);
    expect(groups[0].items.map((i) => i.ordinal)).toEqual([3, 7]);
    expect(groups[1].items.map((i) => i.ordinal)).toEqual([1]);
  });

  it("is empty for no items", () => {
    expect(groupByReason([])).toEqual([]);
  });
});

describe("keyLabel", () => {
  it("prefers the description, then the currency of cash, then an identifier", () => {
    expect(keyLabel(undefined)).toBe("");
    expect(
      keyLabel(create(StatedKeySchema, { description: "ACME CORP" })),
    ).toBe("ACME CORP");
    expect(
      keyLabel(
        create(StatedKeySchema, {
          assetClass: AssetClass.CASH,
          currency: "GBP",
        }),
      ),
    ).toBe("GBP");
    expect(
      keyLabel(
        create(StatedKeySchema, {
          identifiers: [
            create(IdentifierSchema, {
              type: IdentifierType.ISIN,
              value: "US0000000001",
            }),
          ],
        }),
      ),
    ).toBe("US0000000001");
    expect(keyLabel(create(StatedKeySchema, {}))).toBe("");
  });
});
