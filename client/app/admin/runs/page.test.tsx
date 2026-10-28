import { create } from "@bufbuild/protobuf";
import type { ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ReassertTickersService,
  StartReassertTickersResponseSchema,
} from "@/gen/admin/reassert_tickers/v1/reassert_tickers_pb";
import {
  RebuildTickerValidityService,
  StartRebuildTickerValidityResponseSchema,
} from "@/gen/admin/rebuild_ticker_validity/v1/rebuild_ticker_validity_pb";
import {
  RefreshCorpActsService,
  StartRefreshCorpActsResponseSchema,
} from "@/gen/admin/refresh_corp_acts/v1/refresh_corp_acts_pb";
import {
  RunService,
  ListRunsResponseSchema,
  UserRunSchema,
} from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunTrigger } from "@/gen/run/v1/run_pb";
import {
  liveSession,
  renderWithAuth,
  transportWith,
  instant,
  userRun,
} from "@/lib/test-utils";
import RunsPage from "./page";

const router = { push: vi.fn(), replace: vi.fn() };
let params = new URLSearchParams();

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useSearchParams: () => params,
}));

const admin = liveSession({ role: Role.ADMIN });

function serving(
  listRuns: ServiceImpl<typeof RunService>["listRuns"],
  reassertTickers: Partial<ServiceImpl<typeof ReassertTickersService>> = {},
  refresh: Partial<ServiceImpl<typeof RefreshCorpActsService>> = {},
  rebuild: Partial<ServiceImpl<typeof RebuildTickerValidityService>> = {},
) {
  return transportWith(admin, ({ service }) => {
    service(RunService, { listRuns });
    service(ReassertTickersService, reassertTickers);
    service(RefreshCorpActsService, refresh);
    service(RebuildTickerValidityService, rebuild);
  });
}

// A statement with its resolution under it, the resolution holding the
// findings, and an older statement after it.
const page = create(ListRunsResponseSchema, {
  runs: [
    userRun(
      {
        id: "r1",
        kind: RunKind.INGEST_STMT,
        trigger: RunTrigger.USER,
        createdAt: instant("2026-09-24T10:00:00Z"),
      },
      {
        children: [
          userRun(
            {
              id: "r2",
              kind: RunKind.RESOLVE_STMT_KEYS,
              trigger: RunTrigger.RUN,
              parentId: "r1",
              createdAt: instant("2026-09-24T10:00:01Z"),
            },
            { openFindings: 2 },
          ),
        ],
      },
    ),
    userRun({
      id: "r0",
      kind: RunKind.INGEST_STMT,
      trigger: RunTrigger.USER,
      createdAt: instant("2026-09-23T10:00:00Z"),
    }),
  ],
  nextPageToken: "r0",
});

describe("RunsPage", () => {
  beforeEach(() => {
    params = new URLSearchParams();
    router.replace.mockClear();
  });

  it("lists top-level runs closed, with the findings below each, and opens one", async () => {
    renderWithAuth(
      <RunsPage />,
      serving(() => page),
    );
    await waitFor(() => expect(screen.getByTestId("run-row-r1")).toBeTruthy());
    const row = screen.getByTestId("run-row-r1");
    expect(row.textContent).toContain("2026-09-24 10:00 UTC");
    expect(row.textContent).toContain("ingest statement");
    expect(row.textContent).toContain("one@example.com");
    expect(
      row
        .querySelector('[data-testid="state-chip"]')
        ?.getAttribute("data-state"),
    ).toBe("completed");
    expect(row.querySelector("a")?.getAttribute("href")).toBe("/admin/runs/r1");
    expect(screen.queryByTestId("run-row-r2")).toBeNull();
    expect(screen.getByTestId("run-open-findings-r1").textContent).toBe("2");
    expect(screen.queryByTestId("run-toggle-r0")).toBeNull();

    fireEvent.click(screen.getByTestId("run-toggle-r1"));
    expect(screen.getByTestId("run-row-r2").textContent).toContain(
      "resolve statement keys",
    );
    expect(
      screen.getByTestId("run-toggle-r1").getAttribute("aria-expanded"),
    ).toBe("true");
    expect(screen.queryByTestId("run-open-findings-r1")).toBeNull();
    expect(screen.getByTestId("run-open-findings-r2").textContent).toBe("2");
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin/runs?before=r0",
    );
  });

  it("shows a system run with no user to filter by", async () => {
    const system = create(ListRunsResponseSchema, {
      runs: [
        userRun(
          {
            id: "s1",
            kind: RunKind.DATASOURCE_FETCH,
            trigger: RunTrigger.ADMINISTRATOR,
            createdAt: instant("2026-09-24T10:00:00Z"),
          },
          { userId: undefined, userEmail: undefined },
        ),
      ],
    });
    renderWithAuth(
      <RunsPage />,
      serving(() => system),
    );
    await waitFor(() => expect(screen.getByTestId("run-row-s1")).toBeTruthy());
    const row = screen.getByTestId("run-row-s1");
    expect(row.textContent).toContain("System");
    const links = [...row.querySelectorAll("a")].map((a) =>
      a.getAttribute("href"),
    );
    expect(links).toEqual(["/admin/runs/s1"]);
  });

  it("opens the path to a match that a filter reached", async () => {
    params = new URLSearchParams("kind=resolve_stmt_keys");
    const reached = create(ListRunsResponseSchema, {
      runs: [
        create(UserRunSchema, {
          ...page.runs[0],
          matched: false,
        }),
      ],
    });
    renderWithAuth(
      <RunsPage />,
      serving(() => reached),
    );
    await waitFor(() => expect(screen.getByTestId("run-row-r2")).toBeTruthy());
    expect(
      screen.getByTestId("run-toggle-r1").getAttribute("aria-expanded"),
    ).toBe("true");
  });

  it("sends the filters in the address and replaces them on a change", async () => {
    params = new URLSearchParams("kind=ingest_stmt&user=u1");
    const listRuns = vi.fn(() => create(ListRunsResponseSchema, {}));
    renderWithAuth(<RunsPage />, serving(listRuns));
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({ kind: RunKind.INGEST_STMT, userId: "u1" }),
      expect.anything(),
    );
    fireEvent.change(screen.getByTestId("runs-filter-state"), {
      target: { value: "failed" },
    });
    expect(router.replace).toHaveBeenCalledWith(
      "/admin/runs?kind=ingest_stmt&state=failed&user=u1",
    );
  });

  it("reasserts tickers and opens its run", async () => {
    router.push.mockClear();
    const startReassertTickers = vi.fn(() =>
      create(StartReassertTickersResponseSchema, {
        run: create(RunSchema, { id: "x1", kind: RunKind.REASSERT_TICKERS }),
      }),
    );
    renderWithAuth(
      <RunsPage />,
      serving(() => page, { startReassertTickers }),
    );
    fireEvent.click(screen.getByTestId("runs-reassert-tickers"));
    fireEvent.click(screen.getByTestId("reassert-tickers-start"));
    await waitFor(() =>
      expect(router.push).toHaveBeenCalledWith("/admin/runs/x1"),
    );
    expect(startReassertTickers).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("reassert-tickers-dialog")).toBeNull();
  });

  it("refreshes corporate actions and opens its run", async () => {
    router.push.mockClear();
    const startRefreshCorpActs = vi.fn(() =>
      create(StartRefreshCorpActsResponseSchema, {
        run: create(RunSchema, {
          id: "x2",
          kind: RunKind.REFRESH_CORP_ACTS,
        }),
      }),
    );
    renderWithAuth(
      <RunsPage />,
      serving(() => page, {}, { startRefreshCorpActs }),
    );
    fireEvent.click(screen.getByTestId("runs-refresh-corp-acts"));
    fireEvent.click(screen.getByTestId("refresh-corp-acts-start"));
    await waitFor(() =>
      expect(router.push).toHaveBeenCalledWith("/admin/runs/x2"),
    );
    expect(startRefreshCorpActs).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("refresh-corp-acts-dialog")).toBeNull();
  });

  it("rebuilds ticker validity and opens its run", async () => {
    router.push.mockClear();
    const startRebuildTickerValidity = vi.fn(() =>
      create(StartRebuildTickerValidityResponseSchema, {
        run: create(RunSchema, {
          id: "x3",
          kind: RunKind.REBUILD_TICKER_VALIDITY,
        }),
      }),
    );
    renderWithAuth(
      <RunsPage />,
      serving(() => page, {}, {}, { startRebuildTickerValidity }),
    );
    fireEvent.click(screen.getByTestId("runs-rebuild-ticker-validity"));
    fireEvent.click(screen.getByTestId("rebuild-ticker-validity-start"));
    await waitFor(() =>
      expect(router.push).toHaveBeenCalledWith("/admin/runs/x3"),
    );
    expect(startRebuildTickerValidity).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("rebuild-ticker-validity-dialog")).toBeNull();
  });
});
