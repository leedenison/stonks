import { create } from "@bufbuild/protobuf";
import type { ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  AlertGroupSchema,
  AlertKind,
  AlertSchema,
  AlertService,
  GetAlertDigestResponseSchema,
  ListAlertsResponseSchema,
  TickerNamesNothingSchema,
} from "@/gen/admin/alert/v1/alert_pb";
import {
  BlockSchema,
  BlockService,
  ClearBlockResponseSchema,
} from "@/gen/admin/block/v1/block_pb";
import { CorpActSchema } from "@/gen/admin/corpact/v1/corpact_pb";
import {
  ClearFindingResponseSchema,
  ClearRunResponseSchema,
  FindingKind,
  FindingSchema,
  ListRunsResponseSchema,
  RunService as AdminRunService,
} from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { MarketTickerSchema } from "@/gen/type/v1/type_pb";
import {
  liveSession,
  renderWithAuth,
  transportWith,
  userRun,
} from "@/lib/test-utils";
import AdminPage from "./page";

let query = "";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useSearchParams: () => new URLSearchParams(query),
}));
vi.mock("@/app/components/attest-identifier-dialog", () => ({
  AttestIdentifierDialog: (p: { from: string; to?: string }) => (
    <div data-testid="attest-dialog">{`${p.from}/${p.to ?? ""}`}</div>
  ),
}));
vi.mock("@/app/components/decide-corp-act-dialog", () => ({
  DecideCorpActDialog: (p: { corpActId: string }) => (
    <div data-testid="decide-dialog">{p.corpActId}</div>
  ),
}));

const admin = liveSession({ role: Role.ADMIN });

const failedRun = create(AlertSchema, {
  kind: AlertKind.FAILED_RUN,
  subject: { case: "run", value: userRun({ id: "r1", error: "deadline" }) },
});
const block = create(AlertSchema, {
  kind: AlertKind.BLOCK,
  subject: {
    case: "block",
    value: create(BlockSchema, {
      id: "b1",
      datasource: "massive",
      reason: "403",
    }),
  },
});
const names = create(AlertSchema, {
  kind: AlertKind.TICKER_NAMES_NOTHING,
  subject: {
    case: "namesNothing",
    value: create(TickerNamesNothingSchema, {
      ticker: create(MarketTickerSchema, { market: "XNYS", value: "BRK.B" }),
      keys: 3,
      txns: 126,
      firstTxnDay: "2019-03-04",
    }),
  },
});
const corpAct = create(AlertSchema, {
  kind: AlertKind.CORP_ACT,
  subject: {
    case: "corpAct",
    value: create(CorpActSchema, {
      id: "c1",
      exDate: "2024-04-02",
      ratio: { from: "100", to: "104" },
      datasource: "eodhd",
    }),
  },
});
const finding = create(AlertSchema, {
  kind: AlertKind.FINDING,
  subject: {
    case: "finding",
    value: create(FindingSchema, {
      id: "f2",
      kind: FindingKind.CONTRADICTION,
      detail: "two instruments",
    }),
  },
});

const digest = create(GetAlertDigestResponseSchema, {
  groups: [
    create(AlertGroupSchema, {
      kind: AlertKind.FAILED_RUN,
      count: 1,
      top: [failedRun],
    }),
    create(AlertGroupSchema, { kind: AlertKind.BLOCK, count: 7, top: [block] }),
    create(AlertGroupSchema, {
      kind: AlertKind.TICKER_NAMES_NOTHING,
      count: 1,
      top: [names],
    }),
    create(AlertGroupSchema, { kind: AlertKind.TICKER_VALIDITY_GAP, count: 0 }),
    create(AlertGroupSchema, {
      kind: AlertKind.CORP_ACT,
      count: 1,
      top: [corpAct],
    }),
    create(AlertGroupSchema, {
      kind: AlertKind.FINDING,
      count: 1,
      top: [finding],
    }),
  ],
});

const clearBlock = vi.fn(() => create(ClearBlockResponseSchema, {}));
const clearFinding = vi.fn(() => create(ClearFindingResponseSchema, {}));
const clearRun = vi.fn(() => create(ClearRunResponseSchema, {}));
const listAlerts = vi.fn((): unknown =>
  create(ListAlertsResponseSchema, { alerts: [block], nextPageToken: "next" }),
);
const getAlertDigest = vi.fn((): unknown => digest);

function render() {
  renderWithAuth(
    <AdminPage />,
    transportWith(admin, ({ service }) => {
      service(AlertService, {
        getAlertDigest,
        listAlerts,
      } as Partial<ServiceImpl<typeof AlertService>>);
      service(BlockService, {
        clearBlock,
      } satisfies Partial<ServiceImpl<typeof BlockService>>);
      service(AdminRunService, {
        clearFinding,
        clearRun,
        listRuns: () => create(ListRunsResponseSchema, {}),
      } satisfies Partial<ServiceImpl<typeof AdminRunService>>);
    }),
  );
}

beforeEach(() => {
  query = "";
  vi.clearAllMocks();
  getAlertDigest.mockImplementation(() => digest);
});

describe("AdminPage digest", () => {
  it("groups the alerts by kind in rank order and skips an empty kind", async () => {
    render();
    await screen.findByTestId("alert-group-block");
    const heads = screen
      .getAllByTestId(/^alert-group-/)
      .map((e) => e.getAttribute("data-testid"));
    expect(heads).toEqual([
      "alert-group-failed_run",
      "alert-group-block",
      "alert-group-ticker_names_nothing",
      "alert-group-corp_act",
      "alert-group-finding",
    ]);
    expect(
      screen.getByTestId("alert-why-block").getAttribute("title"),
    ).toContain("a datasource is not asked again");
    expect(screen.getByTestId("runs-card")).toBeTruthy();
  });

  it("offers the full list only where the count exceeds the rows shown", async () => {
    render();
    await screen.findByTestId("alert-group-block");
    expect(screen.getByTestId("alert-more-block").getAttribute("href")).toBe(
      "/admin?kind=block",
    );
    expect(screen.queryByTestId("alert-more-failed_run")).toBeNull();
  });

  it("counts every kind in a tile and marks the open tile", async () => {
    render();
    await waitFor(() =>
      expect(screen.getByTestId("alert-tile-open")).toBeTruthy(),
    );
    expect(screen.getByTestId("alert-tile-open").textContent).toContain("11");
    expect(
      screen.getByTestId("alert-tile-open").getAttribute("aria-current"),
    ).toBe("true");
    expect(screen.getByTestId("alert-tile-block").getAttribute("href")).toBe(
      "/admin?kind=block",
    );
    expect(
      screen.getByTestId("alert-tile-block").getAttribute("aria-current"),
    ).toBeNull();
  });

  it("shows an empty state when nothing is open", async () => {
    getAlertDigest.mockImplementation(() =>
      create(GetAlertDigestResponseSchema, {}),
    );
    render();
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
  });

  it("shows an error with a retry", async () => {
    getAlertDigest.mockImplementation(() => {
      throw new Error("down");
    });
    render();
    await waitFor(() =>
      expect(screen.getByText("The alerts could not be loaded.")).toBeTruthy(),
    );
  });

  it("labels the descriptions section as a mock with its action disabled", async () => {
    render();
    const section = await screen.findByTestId("mock-descriptions");
    expect(
      (within(section).getByRole("button") as HTMLButtonElement).disabled,
    ).toBe(true);
  });
});

describe("AdminPage row actions", () => {
  it("clears a failed run", async () => {
    render();
    fireEvent.click(await screen.findByTestId("alert-act-failed_run-r1"));
    await waitFor(() =>
      expect(clearRun).toHaveBeenCalledWith(
        expect.objectContaining({ runId: "r1" }),
        expect.anything(),
      ),
    );
  });

  it("clears a block", async () => {
    render();
    fireEvent.click(await screen.findByTestId("alert-act-block-b1"));
    await waitFor(() =>
      expect(clearBlock).toHaveBeenCalledWith(
        expect.objectContaining({ blockId: "b1" }),
        expect.anything(),
      ),
    );
  });

  it("clears a finding", async () => {
    render();
    fireEvent.click(await screen.findByTestId("alert-act-finding-f2"));
    await waitFor(() =>
      expect(clearFinding).toHaveBeenCalledWith(
        expect.objectContaining({ findingId: "f2" }),
        expect.anything(),
      ),
    );
  });

  it("opens the attest dialog from the first transaction day", async () => {
    render();
    fireEvent.click(
      await screen.findByTestId("alert-act-ticker_names_nothing-XNYS-BRK.B"),
    );
    expect((await screen.findByTestId("attest-dialog")).textContent).toBe(
      "2019-03-04/",
    );
  });

  it("opens the decide dialog on the action", async () => {
    render();
    fireEvent.click(await screen.findByTestId("alert-act-corp_act-c1"));
    expect((await screen.findByTestId("decide-dialog")).textContent).toBe("c1");
  });

  it("says when a block could not be cleared", async () => {
    clearBlock.mockImplementation(() => {
      throw new Error("no");
    });
    render();
    fireEvent.click(await screen.findByTestId("alert-act-block-b1"));
    await waitFor(() =>
      expect(screen.getByText("The alert could not be cleared.")).toBeTruthy(),
    );
  });
});

describe("AdminPage kind list", () => {
  it("lists one kind and pages it", async () => {
    query = "kind=block&before=t1";
    render();
    await waitFor(() =>
      expect(screen.getByTestId("alert-row-block-b1")).toBeTruthy(),
    );
    expect(listAlerts).toHaveBeenCalledWith(
      expect.objectContaining({ kind: AlertKind.BLOCK, pageToken: "t1" }),
      expect.anything(),
    );
    expect(
      screen.getByTestId("alert-tile-block").getAttribute("aria-current"),
    ).toBe("true");
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin?kind=block&before=next",
    );
    expect(screen.getByTestId("list-first").getAttribute("href")).toBe(
      "/admin?kind=block",
    );
    expect(screen.queryByTestId("alert-more-block")).toBeNull();
  });
});
