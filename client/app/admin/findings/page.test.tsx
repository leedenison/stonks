import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  ClearFindingResponseSchema,
  FindingKind,
  FindingSchema,
  ListFindingsResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import FindingsPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const admin = liveSession({ role: Role.ADMIN });
const at = timestampFromDate(new Date("2026-09-24T10:00:00Z"));

function serving(impl: Partial<ServiceImpl<typeof AdminService>>) {
  return transportWith(admin, ({ service }) => {
    service(AdminService, impl);
  });
}

describe("FindingsPage", () => {
  it("leads a block's finding to the blocks page", async () => {
    renderWithAuth(
      <FindingsPage />,
      serving({
        listFindings: () =>
          create(ListFindingsResponseSchema, {
            findings: [
              create(FindingSchema, {
                id: "x1",
                runId: "f1",
                kind: FindingKind.BLOCK,
                blockId: "b1",
                createdAt: at,
              }),
            ],
          }),
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("finding-row-x1")).toBeTruthy(),
    );
    expect(screen.getByTestId("finding-block-x1").getAttribute("href")).toBe(
      "/admin/blocks",
    );
    expect(screen.queryByTestId("finding-clear-x1")).toBeNull();
    expect(
      screen
        .getByTestId("finding-row-x1")
        .querySelector('a[href="/admin/runs/f1"]'),
    ).toBeTruthy();
  });

  it("clears a finding that reports no block", async () => {
    const clearFinding = vi.fn(() => create(ClearFindingResponseSchema, {}));
    renderWithAuth(
      <FindingsPage />,
      serving({
        listFindings: () =>
          create(ListFindingsResponseSchema, {
            findings: [
              create(FindingSchema, { id: "x2", runId: "r1", createdAt: at }),
            ],
          }),
        clearFinding,
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("finding-clear-x2")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("finding-clear-x2"));
    await waitFor(() =>
      expect(clearFinding).toHaveBeenCalledWith(
        expect.objectContaining({ findingId: "x2" }),
        expect.anything(),
      ),
    );
  });
});
