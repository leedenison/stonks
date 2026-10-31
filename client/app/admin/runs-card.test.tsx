import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ListRunsResponseSchema,
  RunService,
  StartSystemRunResponseSchema,
} from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState } from "@/gen/run/v1/run_pb";
import {
  liveSession,
  renderWithAuth,
  transportWith,
  userRun,
} from "@/lib/test-utils";
import { RunsCard } from "./runs-card";
import { systemRunGroups } from "./start-run-dialog";

const systemRuns = systemRunGroups.flatMap((g) => g.runs);

const router = { push: vi.fn(), replace: vi.fn() };

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useSearchParams: () => new URLSearchParams(),
  usePathname: () => "/admin",
}));

function serving(
  listRuns: ServiceImpl<typeof RunService>["listRuns"],
  startSystemRun?: ServiceImpl<typeof RunService>["startSystemRun"],
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RunService, { listRuns, startSystemRun });
  });
}

// A failed reassert run, and no run of the other kinds.
function lastRuns(req: { kind?: RunKind }) {
  return create(ListRunsResponseSchema, {
    runs:
      req.kind === RunKind.REASSERT_TICKERS
        ? [userRun({ id: "x0", kind: req.kind, state: RunState.FAILED })]
        : [],
  });
}

describe("RunsCard", () => {
  beforeEach(() => router.push.mockClear());

  it("lists the runs in group order with the debug run under its heading", async () => {
    renderWithAuth(<RunsCard />, serving(lastRuns));
    const rows = screen
      .getAllByTestId(/^runs-card-(row|group)-/)
      .map((e) => e.getAttribute("data-testid"));
    expect(rows).toEqual([
      "runs-card-row-reassert-tickers",
      "runs-card-row-refresh-corp-acts",
      "runs-card-group-debug",
      "runs-card-row-rebuild-ticker-validity",
    ]);
    for (const r of systemRuns) {
      const row = screen.getByTestId(`runs-card-row-${r.testId}`);
      expect(row.textContent).toContain(r.title);
      expect(
        screen.getByTestId(`runs-card-why-${r.testId}`).getAttribute("title"),
      ).toBe(r.summary);
    }
  });

  it("links the last run by its outcome and says where there is none", async () => {
    renderWithAuth(<RunsCard />, serving(lastRuns));
    const last = await screen.findByTestId("state-chip");
    expect(last.getAttribute("data-state")).toBe("failed");
    expect(
      screen
        .getByTestId("runs-card-last-reassert-tickers")
        .querySelector("a")
        ?.getAttribute("href"),
    ).toBe("/admin/runs/x0");
    await waitFor(() =>
      expect(
        screen.getByTestId("runs-card-last-refresh-corp-acts").textContent,
      ).toBe("Never run"),
    );
  });

  it("says the last run is unknown when it cannot be read", async () => {
    renderWithAuth(
      <RunsCard />,
      serving(() => {
        throw new ConnectError("down", Code.Unavailable);
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByTestId("runs-card-last-reassert-tickers").textContent,
      ).toBe("Unknown"),
    );
  });

  it.each(systemRuns)(
    "starts a $title run, opens it and reads the last run again",
    async (run) => {
      const listed: RunKind[] = [];
      const started: RunKind[] = [];
      renderWithAuth(
        <RunsCard />,
        serving(
          (req) => {
            if (req.kind !== undefined) listed.push(req.kind);
            return lastRuns(req);
          },
          (req) => {
            started.push(req.kind);
            return create(StartSystemRunResponseSchema, {
              run: create(RunSchema, { id: "x1", kind: req.kind }),
            });
          },
        ),
      );
      await waitFor(() => expect(listed).toContain(run.kind));
      fireEvent.click(screen.getByTestId(`runs-card-start-${run.testId}`));
      fireEvent.click(screen.getByTestId(`${run.testId}-start`));
      await waitFor(() =>
        expect(router.push).toHaveBeenCalledWith("/admin/runs/x1"),
      );
      expect(started).toEqual([run.kind]);
      expect(screen.queryByTestId(`${run.testId}-dialog`)).toBeNull();
      await waitFor(() =>
        expect(listed.filter((k) => k === run.kind)).toHaveLength(2),
      );
    },
  );
});
