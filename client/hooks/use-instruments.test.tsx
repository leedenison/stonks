import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  InstrumentSchema,
  InstrumentService,
  ListInstrumentsResponseSchema,
} from "@/gen/instrument/v1/instrument_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useInstruments } from "./use-instruments";

const transport = transportWith(liveSession(), ({ service }) => {
  service(InstrumentService, {
    listInstruments: () =>
      create(ListInstrumentsResponseSchema, {
        instruments: [
          create(InstrumentSchema, { id: "i1", assetClass: AssetClass.CASH }),
        ],
      }),
  });
});

describe("useInstruments", () => {
  it("lists the instruments", async () => {
    const { result } = renderHook(() => useInstruments(), {
      wrapper: authWrapper(transport),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.instruments[0].id).toBe("i1");
  });
});
