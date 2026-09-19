import { AssetClass } from "@/gen/type/v1/type_pb";

// assetClassLabel names an asset class as it is shown to the user.
export function assetClassLabel(assetClass: AssetClass): string {
  switch (assetClass) {
    case AssetClass.CASH:
      return "Cash";
    case AssetClass.SECURITY:
      return "Security";
    case AssetClass.EQUITY:
      return "Equity";
    case AssetClass.STOCK:
      return "Stock";
    case AssetClass.ETF:
      return "ETF";
    case AssetClass.MUTUAL_FUND:
      return "Mutual fund";
    case AssetClass.FIXED_INCOME:
      return "Fixed income";
    case AssetClass.DERIVATIVE:
      return "Derivative";
    case AssetClass.OPTION:
      return "Option";
    case AssetClass.FUTURE:
      return "Future";
    default:
      return "Unknown";
  }
}
