import { create } from "@bufbuild/protobuf";
import type { ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  AdminService,
  ListRunsResponseSchema,
  UserRunSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunTrigger } from "@/gen/run/v1/run_pb";
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

function serving(listRuns: ServiceImpl<typeof AdminService>["listRuns"]) {
  return transportWith(admin, ({ service }) => {
    service(AdminService, { listRuns });
  });
}

// A statement with its resolution under it, the resolution holding the
// findings, and an older statement after it.
const page = create(ListRunsResponseSchema, {
  runs: [
    userRun(
      {
        id: "r1",
        kind: RunKind.STATEMENT,
        trigger: RunTrigger.USER,
        createdAt: instant("2026-09-24T10:00:00Z"),
      },
      {
        children: [
          userRun(
            {
              id: "r2",
              kind: RunKind.RESOLUTION,
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
      kind: RunKind.STATEMENT,
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
    expect(row.textContent).toContain("statement");
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
      "resolution",
    );
    expect(
      screen.getByTestId("run-toggle-r1").getAttribute("aria-expanded"),
    ).toBe("true");
    expect(screen.queryByTestId("run-open-findings-r1")).toBeNull();
    expect(screen.getByTestId("run-open-findings-r2").textContent).toBe("2");
    expect(screen.getByTestId("list-older").getAttribute("href")).toBe(
      "/admin/runs?before=r0",
    );
  });

  it("opens the path to a match that a filter reached", async () => {
    params = new URLSearchParams("kind=resolution");
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
    params = new URLSearchParams("kind=statement&user=u1");
    const listRuns = vi.fn(() => create(ListRunsResponseSchema, {}));
    renderWithAuth(<RunsPage />, serving(listRuns));
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(listRuns).toHaveBeenCalledWith(
      expect.objectContaining({ kind: RunKind.STATEMENT, userId: "u1" }),
      expect.anything(),
    );
    fireEvent.change(screen.getByTestId("runs-filter-state"), {
      target: { value: "failed" },
    });
    expect(router.replace).toHaveBeenCalledWith(
      "/admin/runs?kind=statement&state=failed&user=u1",
    );
  });
});
