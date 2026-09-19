import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import {
  HoldingSchema,
  HoldingService,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { authWrapper } from "@/lib/test-utils";
import { useHoldings } from "./use-holdings";

const live = create(GetSessionResponseSchema, {
  user: create(UserSchema, {
    id: "u1",
    email: "a@example.com",
    role: Role.USER,
  }),
  session: create(SessionSchema, {
    expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
  }),
});

const transport = createRouterTransport(({ service }) => {
  service(AuthService, { getSession: () => live });
  service(HoldingService, {
    listHoldings: () =>
      create(ListHoldingsResponseSchema, {
        holdings: [
          create(HoldingSchema, {
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
    expect(result.current.data?.holdings[0].quantity).toBe("12092.79");
  });
});
