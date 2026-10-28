import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  RebuildTickerValidityService,
  StartRebuildTickerValidityResponseSchema,
} from "@/gen/admin/rebuild_ticker_validity/v1/rebuild_ticker_validity_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { RebuildTickerValidityDialog } from "./rebuild-ticker-validity-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REBUILD_TICKER_VALIDITY,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startRebuildTickerValidity = vi.fn(() =>
    create(StartRebuildTickerValidityResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RebuildTickerValidityService, { startRebuildTickerValidity });
  });
}

describe("RebuildTickerValidityDialog", () => {
  it("rebuilds ticker validity and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <RebuildTickerValidityDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(
      screen.getByTestId("rebuild-ticker-validity-dialog").textContent,
    ).toContain("does not call a datasource");
    fireEvent.click(screen.getByTestId("rebuild-ticker-validity-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startRebuildTickerValidity = vi.fn(() => {
      throw new ConnectError("the run could not start", Code.Internal);
    });
    renderWithAuth(
      <RebuildTickerValidityDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startRebuildTickerValidity),
    );
    fireEvent.click(screen.getByTestId("rebuild-ticker-validity-start"));
    await waitFor(() =>
      expect(screen.getByTestId("rebuild-ticker-validity-error")).toBeTruthy(),
    );
    expect(screen.getByTestId("rebuild-ticker-validity-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
