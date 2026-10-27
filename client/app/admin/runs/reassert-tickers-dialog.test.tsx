import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ReassertTickersService,
  StartReassertTickersResponseSchema,
} from "@/gen/admin/reassert_tickers/v1/reassert_tickers_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { ReassertTickersDialog } from "./reassert-tickers-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REASSERT_TICKERS,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startReassertTickers = vi.fn(() =>
    create(StartReassertTickersResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(ReassertTickersService, { startReassertTickers });
  });
}

describe("ReassertTickersDialog", () => {
  it("reasserts tickers and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <ReassertTickersDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(screen.getByTestId("reassert-tickers-dialog").textContent).toContain(
      "every held ticker",
    );
    fireEvent.click(screen.getByTestId("reassert-tickers-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startReassertTickers = vi.fn(() => {
      throw new ConnectError("no ticker is held", Code.FailedPrecondition);
    });
    renderWithAuth(
      <ReassertTickersDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startReassertTickers),
    );
    fireEvent.click(screen.getByTestId("reassert-tickers-start"));
    await waitFor(() =>
      expect(screen.getByTestId("reassert-tickers-error").textContent).toBe(
        "no ticker is held",
      ),
    );
    expect(screen.getByTestId("reassert-tickers-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
