import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  FetchItemSchema,
  FetchOutcome,
  FindingKind,
  FindingSchema,
  GetRunResponseSchema,
  UserRunSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import {
  IdentifierSchema,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import AdminRunPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  useParams: () => ({ id: "f1" }),
}));

const admin = liveSession({ role: Role.ADMIN });

function serving(getRun: ServiceImpl<typeof AdminService>["getRun"]) {
  return transportWith(admin, ({ service }) => {
    service(AdminService, { getRun });
  });
}

const isin = create(IdentifierSchema, {
  type: IdentifierType.ISIN,
  value: "GB00B03MLX29",
});

const fetch = create(GetRunResponseSchema, {
  run: create(UserRunSchema, {
    run: create(RunSchema, {
      id: "f1",
      kind: RunKind.FETCH,
      trigger: RunTrigger.RUN,
      parentId: "p1",
      state: RunState.COMPLETED,
      createdAt: timestampFromDate(new Date("2026-09-24T10:00:00Z")),
    }),
    userId: "u1",
    userEmail: "one@example.com",
  }),
  findings: [
    create(FindingSchema, {
      id: "x1",
      runId: "f1",
      kind: FindingKind.BLOCK,
      blockId: "b1",
      createdAt: timestampFromDate(new Date("2026-09-24T10:01:00Z")),
    }),
  ],
  fetchItems: [
    create(FetchItemSchema, {
      statedKey: create(StatedKeySchema, {
        identifiers: [isin],
        description: "SHELL PLC",
      }),
      statedKeyId: "k1",
      sent: isin,
      outcome: FetchOutcome.FAILED_PERMANENT,
      attempts: 1,
      reason: "unknown identifier",
    }),
  ],
});

describe("AdminRunPage", () => {
  it("shows the run, its parent, its findings and its items", async () => {
    renderWithAuth(
      <AdminRunPage />,
      serving(() => fetch),
    );
    await waitFor(() =>
      expect(screen.getByTestId("admin-run-summary")).toBeTruthy(),
    );
    expect(screen.getByTestId("page-title").textContent).toBe(
      "fetch run @ 2026-09-24 10:00 UTC",
    );
    expect(screen.getByTestId("admin-run-parent").getAttribute("href")).toBe(
      "/admin/runs/p1",
    );
    expect(screen.getByTestId("finding-row-x1").textContent).toContain("block");
    const item = screen.getByTestId("item-row-k1");
    expect(item.textContent).toContain("ISIN GB00B03MLX29 · SHELL PLC");
    expect(item.textContent).toContain("failed permanent");
    expect(item.textContent).toContain("unknown identifier");
  });

  it("says when there is no such run", async () => {
    renderWithAuth(
      <AdminRunPage />,
      serving(() => {
        throw new ConnectError("no such run", Code.NotFound);
      }),
    );
    await waitFor(() =>
      expect(screen.getByText("There is no run at this address.")).toBeTruthy(),
    );
  });
});
