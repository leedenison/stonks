import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  ClearBlockResponseSchema,
  ClearFindingResponseSchema,
  FetchItemSchema,
  FetchOutcome,
  DropStep,
  FindingKind,
  FindingSchema,
  GetRunResponseSchema,
  ListDatasourcesResponseSchema,
  ListRunItemsResponseSchema,
  ReplaySchema,
  RunItemSchema,
  StartReplayResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunTrigger } from "@/gen/run/v1/run_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import {
  liveSession,
  renderWithAuth,
  transportWith,
  instant,
  userRun,
} from "@/lib/test-utils";
import AdminRunPage from "./page";

const router = { push: vi.fn() };

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useParams: () => ({ id: "f1" }),
}));

const admin = liveSession({ role: Role.ADMIN });

// serving answers getRun as given and lists no items unless impl says
// otherwise.
function serving(
  getRun: ServiceImpl<typeof AdminService>["getRun"],
  impl: Partial<ServiceImpl<typeof AdminService>> = {},
) {
  return transportWith(admin, ({ service }) => {
    service(AdminService, {
      getRun,
      listRunItems: () => create(ListRunItemsResponseSchema, {}),
      ...impl,
    });
  });
}

const isin = create(IdentifierSchema, {
  type: IdentifierType.ISIN,
  value: "GB00B03MLX29",
});
const ticker = create(IdentifierSchema, {
  type: IdentifierType.MIC_TICKER,
  domain: "XLON",
  value: "SHEL",
});
const shell = create(IdentifierSchema, {
  type: IdentifierType.BROKER_DESCRIPTION,
  domain: "ibkr",
  value: "SHELL PLC",
});

// A fetch under a resolution under a statement, the fetch being the run
// read, with one run below it.
const fetch = create(GetRunResponseSchema, {
  run: userRun(
    {
      id: "f1",
      kind: RunKind.FETCH,
      trigger: RunTrigger.RUN,
      parentId: "p1",
      createdAt: instant("2026-09-24T10:00:00Z"),
    },
    {
      children: [
        userRun(
          {
            id: "c1",
            kind: RunKind.FETCH,
            trigger: RunTrigger.RUN,
            parentId: "f1",
            createdAt: instant("2026-09-24T10:00:05Z"),
          },
          { openFindings: 1 },
        ),
      ],
    },
  ),
  ancestors: [
    userRun({
      id: "s1",
      kind: RunKind.STATEMENT,
      trigger: RunTrigger.USER,
      createdAt: instant("2026-09-24T09:59:00Z"),
    }),
    userRun({
      id: "p1",
      kind: RunKind.RESOLUTION,
      trigger: RunTrigger.RUN,
      parentId: "s1",
      createdAt: instant("2026-09-24T09:59:30Z"),
    }),
  ],
  findings: [
    create(FindingSchema, {
      id: "x1",
      runId: "f1",
      kind: FindingKind.BLOCK,
      blockId: "b1",
      detail: "openfigi rejected the identifier: Invalid idValue format.",
      createdAt: timestampFromDate(new Date("2026-09-24T10:01:00Z")),
    }),
    create(FindingSchema, {
      id: "x2",
      runId: "c1",
      kind: FindingKind.DROPPED,
      statedKeyId: "k1",
      fetchKeyId: "fk1",
      step: DropStep.STATED,
      detail: "candidates in USD, not the stated GBP",
      statedKey: create(StatedKeySchema, {
        identifiers: [isin, ticker, shell],
        assetClass: AssetClass.EQUITY,
        currency: "GBP",
      }),
      createdAt: timestampFromDate(new Date("2026-09-24T10:01:00Z")),
    }),
  ],
});

const fetchItems = create(ListRunItemsResponseSchema, {
  items: [
    create(RunItemSchema, {
      item: {
        case: "fetch",
        value: create(FetchItemSchema, {
          statedKey: create(StatedKeySchema, {
            identifiers: [isin, shell],
          }),
          statedKeyId: "k1",
          sent: isin,
          outcome: FetchOutcome.FAILED_PERMANENT,
          attempts: 1,
          reason: "unknown identifier",
        }),
      },
    }),
  ],
});

describe("AdminRunPage", () => {
  it("shows the run among its ancestors, its findings and its items", async () => {
    renderWithAuth(
      <AdminRunPage />,
      serving(() => fetch, { listRunItems: () => fetchItems }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("admin-run-lineage")).toBeTruthy(),
    );
    expect(screen.getByTestId("page-title").textContent).toBe(
      "fetch run @ 2026-09-24 10:00 UTC",
    );
    const lineage = screen.getByTestId("admin-run-lineage");
    const shown = () =>
      Array.from(lineage.querySelectorAll("tbody tr")).map((tr) =>
        tr.getAttribute("data-testid"),
      );
    expect(shown()).toEqual(["run-row-s1", "run-row-p1", "run-row-f1"]);
    const self = screen.getByTestId("run-row-f1");
    expect(self.getAttribute("aria-current")).toBe("page");
    expect(self.textContent).toContain("one@example.com");
    expect(self.querySelector('a[href="/admin/runs/f1"]')).toBeNull();
    expect(
      screen.getByTestId("run-row-p1").querySelector("a")?.getAttribute("href"),
    ).toBe("/admin/runs/p1");
    // Closed, the run counts the findings of the run it hides; open, each
    // row counts its own.
    expect(screen.getByTestId("run-open-findings-f1").textContent).toBe("1");
    fireEvent.click(screen.getByTestId("run-toggle-f1"));
    expect(shown()).toEqual([
      "run-row-s1",
      "run-row-p1",
      "run-row-f1",
      "run-row-c1",
    ]);
    expect(screen.queryByTestId("run-open-findings-f1")).toBeNull();
    expect(screen.getByTestId("run-open-findings-c1").textContent).toBe("1");
    fireEvent.click(screen.getByTestId("run-toggle-p1"));
    expect(shown()).toEqual(["run-row-s1", "run-row-p1"]);
    expect(screen.getByTestId("finding-row-x1").textContent).toContain(
      "fetch @ 2026-09-24 10:00 UTC",
    );
    expect(
      screen.getByTestId("finding-row-x1").querySelector("a[href]"),
    ).toBeNull();
    expect(screen.getByTestId("finding-row-x1").textContent).toContain("block");
    expect(screen.getByTestId("finding-row-x1").textContent).toContain(
      "openfigi rejected the identifier: Invalid idValue format.",
    );
    expect(screen.getByTestId("finding-run-c1").getAttribute("href")).toBe(
      "/admin/runs/c1",
    );
    expect(screen.getByTestId("finding-row-x2").textContent).toContain(
      "stated: candidates in USD, not the stated GBP",
    );
    expect(screen.getByTestId("finding-row-x2").textContent).not.toContain(
      "10:01 UTC",
    );

    // One finding opens at a time, from its toggle or its row.
    expect(screen.queryByTestId("finding-detail-x1")).toBeNull();
    fireEvent.click(screen.getByTestId("finding-toggle-x1"));
    expect(screen.getByTestId("finding-detail-x1").textContent).toBe(
      "No stated key.",
    );
    fireEvent.click(screen.getByTestId("finding-row-x2"));
    expect(screen.queryByTestId("finding-detail-x1")).toBeNull();
    const detail = screen.getByTestId("finding-detail-x2");
    expect(detail.querySelectorAll("[data-identifier-type]")).toHaveLength(3);
    expect(detail.textContent).toContain("GB00B03MLX29");
    expect(detail.textContent).toContain("SHEL(XLON)");
    expect(detail.textContent).toContain("SHELL PLC");
    expect(detail.textContent).toContain("Equity");
    expect(detail.textContent).toContain("GBP");
    expect(detail.textContent).not.toContain("fk1");
    fireEvent.click(screen.getByTestId("finding-row-x2"));
    expect(screen.queryByTestId("finding-detail-x2")).toBeNull();

    const item = await screen.findByTestId("item-row-k1");
    const chips = item.querySelectorAll("[data-identifier-type='ISIN']");
    expect(chips).toHaveLength(2);
    expect(item.textContent).toContain("GB00B03MLX29");
    expect(item.textContent).toContain("SHELL PLC");
    expect(item.textContent).toContain("failed permanent");
    expect(item.textContent).toContain("unknown identifier");
  });

  it("clears a finding, and a block's finding through its block", async () => {
    const clearFinding = vi.fn(() => create(ClearFindingResponseSchema, {}));
    const clearBlock = vi.fn(() => create(ClearBlockResponseSchema, {}));
    renderWithAuth(
      <AdminRunPage />,
      serving(() => fetch, { clearFinding, clearBlock }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("finding-clear-block-x1")).toBeTruthy(),
    );
    expect(screen.getByTestId("finding-clear-block-x1").tagName).toBe("BUTTON");
    expect(screen.queryByTestId("finding-clear-x1")).toBeNull();
    expect(screen.queryByTestId("finding-clear-block-x2")).toBeNull();

    fireEvent.click(screen.getByTestId("finding-clear-block-x1"));
    await waitFor(() =>
      expect(clearBlock).toHaveBeenCalledWith(
        expect.objectContaining({ blockId: "b1" }),
        expect.anything(),
      ),
    );
    await waitFor(() =>
      expect(
        (screen.getByTestId("finding-clear-x2") as HTMLButtonElement).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByTestId("finding-clear-x2"));
    await waitFor(() =>
      expect(clearFinding).toHaveBeenCalledWith(
        expect.objectContaining({ findingId: "x2" }),
        expect.anything(),
      ),
    );
  });

  it("shows when a finding was cleared in place of its clear", async () => {
    const cleared = create(GetRunResponseSchema, {
      ...fetch,
      findings: [
        create(FindingSchema, {
          ...fetch.findings[1],
          clearedAt: timestampFromDate(new Date("2026-09-24T11:00:00Z")),
        }),
      ],
    });
    renderWithAuth(
      <AdminRunPage />,
      serving(() => cleared),
    );
    await waitFor(() =>
      expect(screen.getByTestId("finding-row-x2")).toBeTruthy(),
    );
    expect(screen.getByTestId("finding-row-x2").textContent).toContain(
      "2026-09-24 11:00 UTC",
    );
    expect(screen.queryByTestId("finding-clear-x2")).toBeNull();
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
  it("shows the tree of the run a replay re-resolved", async () => {
    const replay = create(GetRunResponseSchema, {
      run: userRun({
        id: "f1",
        kind: RunKind.REPLAY,
        trigger: RunTrigger.ADMINISTRATOR,
        createdAt: instant("2026-09-24T10:00:00Z"),
      }),
      replay: create(ReplaySchema, {
        sourceRunId: "s1",
        startedBy: "admin@example.com",
      }),
    });
    // The source, a statement with its resolution under it.
    const source = create(GetRunResponseSchema, {
      run: userRun(
        {
          id: "s1",
          kind: RunKind.STATEMENT,
          trigger: RunTrigger.USER,
          createdAt: instant("2026-09-23T10:00:00Z"),
        },
        {
          children: [
            userRun({
              id: "s2",
              kind: RunKind.RESOLUTION,
              trigger: RunTrigger.RUN,
              parentId: "s1",
            }),
          ],
        },
      ),
    });
    renderWithAuth(
      <AdminRunPage />,
      serving((req) => (req.runId === "s1" ? source : replay)),
    );
    await waitFor(() =>
      expect(screen.getByTestId("admin-run-source")).toBeTruthy(),
    );
    const tree = screen.getByTestId("admin-run-source");
    const row = tree.querySelector('[data-testid="run-row-s1"]');
    expect(row?.textContent).toContain("statement");
    expect(row?.getAttribute("aria-current")).toBeNull();
    expect(row?.querySelector("a")?.getAttribute("href")).toBe(
      "/admin/runs/s1",
    );
    expect(tree.querySelector('[data-testid="run-row-s2"]')).toBeNull();
    fireEvent.click(screen.getByTestId("run-toggle-s1"));
    expect(tree.querySelector('[data-testid="run-row-s2"]')).toBeTruthy();
    expect(screen.queryByTestId("run-replay")).toBeNull();
  });

  it("replays a statement's keys from its action bar and goes to the run", async () => {
    const statement = create(GetRunResponseSchema, {
      run: userRun({
        id: "f1",
        kind: RunKind.STATEMENT,
        trigger: RunTrigger.USER,
        createdAt: instant("2026-09-24T10:00:00Z"),
      }),
    });
    const startReplay = vi.fn(() =>
      create(StartReplayResponseSchema, {
        run: create(RunSchema, { id: "x1", kind: RunKind.REPLAY }),
      }),
    );
    router.push.mockClear();
    renderWithAuth(
      <AdminRunPage />,
      serving(() => statement, {
        listDatasources: () => create(ListDatasourcesResponseSchema, {}),
        startReplay,
      }),
    );
    await waitFor(() => expect(screen.getByTestId("run-replay")).toBeTruthy());
    expect(screen.queryByTestId("replay-dialog")).toBeNull();
    fireEvent.click(screen.getByTestId("run-replay"));
    expect(screen.getByTestId("replay-dialog").textContent).toContain(
      "statement run @ 2026-09-24 10:00 UTC for one@example.com",
    );
    fireEvent.click(screen.getByTestId("replay-start"));
    await waitFor(() =>
      expect(router.push).toHaveBeenCalledWith("/admin/runs/x1"),
    );
    expect(startReplay).toHaveBeenCalledWith(
      expect.objectContaining({
        runId: "f1",
        scope: { case: "unavailable", value: true },
      }),
      expect.anything(),
    );
    expect(screen.queryByTestId("replay-dialog")).toBeNull();
  });
  it("reads further items a page at a time", async () => {
    const page = (key: string, next: string) =>
      create(ListRunItemsResponseSchema, {
        items: [
          create(RunItemSchema, {
            item: {
              case: "fetch",
              value: create(FetchItemSchema, {
                statedKey: create(StatedKeySchema, { identifiers: [isin] }),
                statedKeyId: key,
                outcome: FetchOutcome.SERVED,
                attempts: 1,
              }),
            },
          }),
        ],
        nextPageToken: next,
      });
    renderWithAuth(
      <AdminRunPage />,
      serving(() => fetch, {
        listRunItems: (req) =>
          req.pageToken === "k1" ? page("k2", "") : page("k1", "k1"),
      }),
    );
    await screen.findByTestId("item-row-k1");
    expect(screen.queryByTestId("item-row-k2")).toBeNull();
    fireEvent.click(screen.getByTestId("admin-run-items-more"));
    await screen.findByTestId("item-row-k2");
    expect(screen.getByTestId("item-row-k1")).toBeTruthy();
    expect(screen.queryByTestId("admin-run-items-more")).toBeNull();
  });
});
