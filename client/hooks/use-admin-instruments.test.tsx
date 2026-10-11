import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  InstrumentSchema,
  InstrumentService,
  ListInstrumentsResponseSchema,
} from "@/gen/admin/instrument/v1/instrument_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useAdminInstruments } from "./use-admin-instruments";

const listInstruments = vi.fn(() =>
  create(ListInstrumentsResponseSchema, {
    instruments: [
      create(InstrumentSchema, {
        id: "i1",
        name: "USD",
        assetClass: AssetClass.CASH,
      }),
    ],
  }),
);

const transport = transportWith(
  liveSession({ role: Role.ADMIN }),
  ({ service }) => {
    service(InstrumentService, { listInstruments });
  },
);

describe("useAdminInstruments", () => {
  it("lists the page after the token", async () => {
    const { result } = renderHook(() => useAdminInstruments("t1"), {
      wrapper: authWrapper(transport),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.instruments[0].name).toBe("USD");
    expect(listInstruments).toHaveBeenCalledWith(
      expect.objectContaining({ pageToken: "t1" }),
      expect.anything(),
    );
  });
});
