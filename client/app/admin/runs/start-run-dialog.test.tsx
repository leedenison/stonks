import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  RunService,
  StartSystemRunResponseSchema,
} from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { StartRunDialog, systemRuns } from "./start-run-dialog";

function serving(
  startSystemRun: ServiceImpl<typeof RunService>["startSystemRun"],
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RunService, { startSystemRun });
  });
}

describe("StartRunDialog", () => {
  it.each(systemRuns)("starts a $title run and hands it over", async (run) => {
    const started = create(RunSchema, {
      id: "x1",
      kind: run.kind,
      trigger: RunTrigger.ADMINISTRATOR,
      state: RunState.PENDING,
    });
    const kinds: RunKind[] = [];
    const onStarted = vi.fn();
    renderWithAuth(
      <StartRunDialog run={run} onClose={vi.fn()} onStarted={onStarted} />,
      serving((req) => {
        kinds.push(req.kind);
        return create(StartSystemRunResponseSchema, { run: started });
      }),
    );
    expect(screen.getByTestId(`${run.testId}-dialog`).textContent).toContain(
      run.description,
    );
    fireEvent.click(screen.getByTestId(`${run.testId}-start`));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
    expect(kinds).toEqual([run.kind]);
  });

  it("shows a refusal and stays open", async () => {
    const run = systemRuns[0];
    const onStarted = vi.fn();
    renderWithAuth(
      <StartRunDialog run={run} onClose={vi.fn()} onStarted={onStarted} />,
      serving(() => {
        throw new ConnectError("no ticker is held", Code.FailedPrecondition);
      }),
    );
    fireEvent.click(screen.getByTestId(`${run.testId}-start`));
    await waitFor(() =>
      expect(screen.getByTestId(`${run.testId}-error`).textContent).toBe(
        "no ticker is held",
      ),
    );
    expect(screen.getByTestId(`${run.testId}-dialog`)).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
