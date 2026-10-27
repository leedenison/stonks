import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  DatasourceSchema,
  DatasourceService,
  ListDatasourcesResponseSchema,
} from "@/gen/admin/datasource/v1/datasource_pb";
import {
  ReplayRunService,
  StartReplayRunResponseSchema,
} from "@/gen/admin/replay_run/v1/replay_run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import {
  liveSession,
  renderWithAuth,
  transportWith,
  instant,
  userRun,
} from "@/lib/test-utils";
import { ReplayRunDialog } from "./replay-run-dialog";

const statement = userRun({
  id: "r1",
  kind: RunKind.INGEST_STMT,
  trigger: RunTrigger.USER,
  createdAt: instant("2026-09-24T10:00:00Z"),
});

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REPLAY_RUN,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

const starting = () => create(StartReplayRunResponseSchema, { run: started });

function serving(startReplayRun = vi.fn(starting)) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(DatasourceService, {
      listDatasources: () =>
        create(ListDatasourcesResponseSchema, {
          datasources: [
            create(DatasourceSchema, { name: "openfigi", enabled: true }),
            create(DatasourceSchema, { name: "other", enabled: false }),
          ],
        }),
    });
    service(ReplayRunService, { startReplayRun });
  });
}

describe("ReplayRunDialog", () => {
  it("replays the keys left unavailable of the run", async () => {
    const startReplayRun = vi.fn(starting);
    const onStarted = vi.fn();
    renderWithAuth(
      <ReplayRunDialog
        run={statement}
        onClose={vi.fn()}
        onStarted={onStarted}
      />,
      serving(startReplayRun),
    );
    expect(screen.getByTestId("replay-dialog").textContent).toContain(
      "ingest statement @ 2026-09-24 10:00 UTC for one@example.com",
    );
    fireEvent.click(screen.getByTestId("replay-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
    expect(startReplayRun).toHaveBeenCalledWith(
      expect.objectContaining({
        runId: "r1",
        scope: { case: "unavailable", value: true },
      }),
      expect.anything(),
    );
  });

  it("replays the keys an enabled datasource has not yet answered", async () => {
    const startReplayRun = vi.fn(starting);
    renderWithAuth(
      <ReplayRunDialog run={statement} onClose={vi.fn()} onStarted={vi.fn()} />,
      serving(startReplayRun),
    );
    const select = screen.getByTestId("replay-datasource");
    await waitFor(() =>
      expect(select.querySelectorAll("option")).toHaveLength(1),
    );
    expect(select.textContent).toBe("openfigi");
    expect((select as HTMLSelectElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("replay-scope-datasource"));
    expect((select as HTMLSelectElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("replay-start"));
    await waitFor(() =>
      expect(startReplayRun).toHaveBeenCalledWith(
        expect.objectContaining({
          runId: "r1",
          scope: { case: "datasource", value: "openfigi" },
        }),
        expect.anything(),
      ),
    );
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startReplayRun = vi.fn(() => {
      throw new ConnectError("no key to replay", Code.FailedPrecondition);
    });
    renderWithAuth(
      <ReplayRunDialog
        run={statement}
        onClose={vi.fn()}
        onStarted={onStarted}
      />,
      serving(startReplayRun),
    );
    fireEvent.click(screen.getByTestId("replay-start"));
    await waitFor(() =>
      expect(screen.getByTestId("replay-error").textContent).toBe(
        "no key to replay",
      ),
    );
    expect(screen.getByTestId("replay-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
