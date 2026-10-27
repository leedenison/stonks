import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  RebuildResolvedTickersService,
  StartRebuildResolvedTickersResponseSchema,
} from "@/gen/admin/rebuild_resolved_tickers/v1/rebuild_resolved_tickers_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { RebuildResolvedTickersDialog } from "./rebuild-resolved-tickers-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REBUILD_RESOLVED_TICKERS,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startRebuildResolvedTickers = vi.fn(() =>
    create(StartRebuildResolvedTickersResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RebuildResolvedTickersService, { startRebuildResolvedTickers });
  });
}

describe("RebuildResolvedTickersDialog", () => {
  it("rebuilds ticker names and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <RebuildResolvedTickersDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(
      screen.getByTestId("rebuild-resolved-tickers-dialog").textContent,
    ).toContain("calls no datasource");
    fireEvent.click(screen.getByTestId("rebuild-resolved-tickers-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startRebuildResolvedTickers = vi.fn(() => {
      throw new ConnectError("the run could not start", Code.Internal);
    });
    renderWithAuth(
      <RebuildResolvedTickersDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startRebuildResolvedTickers),
    );
    fireEvent.click(screen.getByTestId("rebuild-resolved-tickers-start"));
    await waitFor(() =>
      expect(screen.getByTestId("rebuild-resolved-tickers-error")).toBeTruthy(),
    );
    expect(screen.getByTestId("rebuild-resolved-tickers-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
