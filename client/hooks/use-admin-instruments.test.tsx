import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  InstrumentSchema,
  InstrumentService,
  ListInstrumentsResponseSchema,
} from "@/gen/admin/instrument/v1/instrument_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { AssetClass, IdentifierType } from "@/gen/type/v1/type_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useAdminInstruments } from "./use-admin-instruments";

const listInstruments = vi.fn(() =>
  create(ListInstrumentsResponseSchema, {
    instruments: [
      create(InstrumentSchema, {
        id: "i1",
        name: { type: IdentifierType.CURRENCY, value: "USD" },
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
    const { result } = renderHook(
      () =>
        useAdminInstruments({ after: "t1", id: "", q: "us", class: "cash" }),
      { wrapper: authWrapper(transport) },
    );
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.instruments[0].name?.value).toBe("USD");
    expect(listInstruments).toHaveBeenCalledWith(
      expect.objectContaining({
        pageToken: "t1",
        query: "us",
        assetClass: AssetClass.CASH,
      }),
      expect.anything(),
    );
  });
});
