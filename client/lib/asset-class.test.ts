import { describe, expect, it } from "vitest";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { assetClassLabel } from "./asset-class";

describe("assetClassLabel", () => {
  it("names each class, and the root and an unset class alike", () => {
    expect(assetClassLabel(AssetClass.CASH)).toBe("Cash");
    expect(assetClassLabel(AssetClass.MUTUAL_FUND)).toBe("Mutual fund");
    expect(assetClassLabel(AssetClass.UNKNOWN)).toBe("Unknown");
    expect(assetClassLabel(AssetClass.UNSPECIFIED)).toBe("Unknown");
  });
});
