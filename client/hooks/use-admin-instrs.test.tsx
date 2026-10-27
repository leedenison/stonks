import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  InstrSchema,
  InstrService,
  ListInstrsResponseSchema,
} from "@/gen/admin/instr/v1/instr_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { AssetClass, IdentifierType } from "@/gen/type/v1/type_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useAdminInstrs } from "./use-admin-instrs";

const listInstrs = vi.fn(() =>
  create(ListInstrsResponseSchema, {
    instrs: [
      create(InstrSchema, {
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
    service(InstrService, { listInstrs });
  },
);

describe("useAdminInstrs", () => {
  it("lists the page after the token", async () => {
    const { result } = renderHook(
      () => useAdminInstrs({ after: "t1", q: "us", class: "cash" }),
      { wrapper: authWrapper(transport) },
    );
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.instrs[0].name?.value).toBe("USD");
    expect(listInstrs).toHaveBeenCalledWith(
      expect.objectContaining({
        pageToken: "t1",
        query: "us",
        assetClass: AssetClass.CASH,
      }),
      expect.anything(),
    );
  });
});
