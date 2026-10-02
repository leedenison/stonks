import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  DatasourceSchema,
  ListDatasourcesResponseSchema,
  StartReplayResponseSchema,
  UserRunSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { ReplayDialog } from "./replay-dialog";

const statement = create(UserRunSchema, {
  run: create(RunSchema, {
    id: "r1",
    kind: RunKind.STATEMENT,
    trigger: RunTrigger.USER,
    state: RunState.COMPLETED,
    createdAt: timestampFromDate(new Date("2026-09-24T10:00:00Z")),
  }),
  userId: "u1",
  userEmail: "one@example.com",
});

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REPLAY,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

const starting = () => create(StartReplayResponseSchema, { run: started });

function serving(startReplay = vi.fn(starting)) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(AdminService, {
      listDatasources: () =>
        create(ListDatasourcesResponseSchema, {
          datasources: [
            create(DatasourceSchema, { name: "openfigi", enabled: true }),
            create(DatasourceSchema, { name: "other", enabled: false }),
          ],
        }),
      startReplay,
    });
  });
}

describe("ReplayDialog", () => {
  it("replays the keys left unavailable of the run", async () => {
    const startReplay = vi.fn(starting);
    const onStarted = vi.fn();
    renderWithAuth(
      <ReplayDialog run={statement} onClose={vi.fn()} onStarted={onStarted} />,
      serving(startReplay),
    );
    expect(screen.getByTestId("replay-dialog").textContent).toContain(
      "statement run @ 2026-09-24 10:00 UTC for one@example.com",
    );
    fireEvent.click(screen.getByTestId("replay-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
    expect(startReplay).toHaveBeenCalledWith(
      expect.objectContaining({
        runId: "r1",
        scope: { case: "unavailable", value: true },
      }),
      expect.anything(),
    );
  });

  it("replays the keys an enabled datasource has not yet answered", async () => {
    const startReplay = vi.fn(starting);
    renderWithAuth(
      <ReplayDialog run={statement} onClose={vi.fn()} onStarted={vi.fn()} />,
      serving(startReplay),
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
      expect(startReplay).toHaveBeenCalledWith(
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
    const startReplay = vi.fn(() => {
      throw new ConnectError("no key to replay", Code.FailedPrecondition);
    });
    renderWithAuth(
      <ReplayDialog run={statement} onClose={vi.fn()} onStarted={onStarted} />,
      serving(startReplay),
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
