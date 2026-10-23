import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ExtendValidityService,
  StartExtendValidityResponseSchema,
} from "@/gen/admin/extend_validity/v1/extend_validity_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { ExtendValidityDialog } from "./extend-validity-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.EXTEND_VALIDITY,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startExtendValidity = vi.fn(() =>
    create(StartExtendValidityResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(ExtendValidityService, { startExtendValidity });
  });
}

describe("ExtendValidityDialog", () => {
  it("extends validity and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <ExtendValidityDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(screen.getByTestId("extend-validity-dialog").textContent).toContain(
      "every held ticker",
    );
    fireEvent.click(screen.getByTestId("extend-validity-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startExtendValidity = vi.fn(() => {
      throw new ConnectError("no ticker is held", Code.FailedPrecondition);
    });
    renderWithAuth(
      <ExtendValidityDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startExtendValidity),
    );
    fireEvent.click(screen.getByTestId("extend-validity-start"));
    await waitFor(() =>
      expect(screen.getByTestId("extend-validity-error").textContent).toBe(
        "no ticker is held",
      ),
    );
    expect(screen.getByTestId("extend-validity-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
