import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  BlockSchema,
  BlockScope,
  ClearBlockResponseSchema,
  FetchKind,
  ListBlocksResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { IdentifierSchema, IdentifierType } from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import BlocksPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const admin = liveSession({ role: Role.ADMIN });
const at = timestampFromDate(new Date("2026-09-24T10:00:00Z"));

const listed = create(ListBlocksResponseSchema, {
  blocks: [
    create(BlockSchema, {
      id: "b1",
      datasource: "openfigi",
      kind: FetchKind.IDENTITY,
      scope: BlockScope.IDENTIFIER,
      sent: create(IdentifierSchema, {
        type: IdentifierType.ISIN,
        value: "GB00B03MLX29",
      }),
      reason: "unknown identifier",
      runId: "f1",
      createdAt: at,
    }),
    create(BlockSchema, {
      id: "b2",
      datasource: "openfigi",
      kind: FetchKind.IDENTITY,
      scope: BlockScope.DATASOURCE,
      reason: "quota spent",
      runId: "f2",
      createdAt: at,
      clearedAt: at,
    }),
  ],
});

describe("BlocksPage", () => {
  it("lists blocks and clears an open one", async () => {
    const clearBlock = vi.fn(() => create(ClearBlockResponseSchema, {}));
    renderWithAuth(
      <BlocksPage />,
      transportWith(admin, ({ service }) => {
        service(AdminService, {
          listBlocks: () => listed,
          clearBlock,
        } satisfies Partial<ServiceImpl<typeof AdminService>>);
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("block-row-b1")).toBeTruthy(),
    );
    expect(screen.getByTestId("block-row-b1").textContent).toContain(
      "ISIN GB00B03MLX29",
    );
    expect(screen.getByTestId("block-row-b2").textContent).toContain(
      "every call",
    );
    expect(screen.queryByTestId("block-clear-b2")).toBeNull();
    fireEvent.click(screen.getByTestId("block-clear-b1"));
    await waitFor(() =>
      expect(clearBlock).toHaveBeenCalledWith(
        expect.objectContaining({ blockId: "b1" }),
        expect.anything(),
      ),
    );
  });
});
