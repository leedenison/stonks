import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  InstrumentHoldingSchema,
  HoldingService,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useHoldings } from "./use-holdings";

const transport = transportWith(liveSession(), ({ service }) => {
  service(HoldingService, {
    listHoldings: () =>
      create(ListHoldingsResponseSchema, {
        instruments: [
          create(InstrumentHoldingSchema, {
            instrumentId: "i1",
            assetClass: AssetClass.CASH,
            quantity: "12092.79",
          }),
        ],
      }),
  });
});

describe("useHoldings", () => {
  it("lists the holdings", async () => {
    const { result } = renderHook(() => useHoldings(), {
      wrapper: authWrapper(transport),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.instruments[0].quantity).toBe("12092.79");
  });
});
