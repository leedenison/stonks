import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  RefreshCorporateEventsService,
  StartRefreshCorporateEventsResponseSchema,
} from "@/gen/admin/refresh_corporate_events/v1/refresh_corporate_events_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { RefreshCorporateEventsDialog } from "./refresh-corporate-events-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REFRESH_CORPORATE_EVENTS,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startRefreshCorporateEvents = vi.fn(() =>
    create(StartRefreshCorporateEventsResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RefreshCorporateEventsService, { startRefreshCorporateEvents });
  });
}

describe("RefreshCorporateEventsDialog", () => {
  it("refreshes corporate events and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <RefreshCorporateEventsDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(
      screen.getByTestId("refresh-corporate-events-dialog").textContent,
    ).toContain("ticker that named a held instrument");
    fireEvent.click(screen.getByTestId("refresh-corporate-events-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startRefreshCorporateEvents = vi.fn(() => {
      throw new ConnectError("no instrument is held", Code.FailedPrecondition);
    });
    renderWithAuth(
      <RefreshCorporateEventsDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startRefreshCorporateEvents),
    );
    fireEvent.click(screen.getByTestId("refresh-corporate-events-start"));
    await waitFor(() =>
      expect(
        screen.getByTestId("refresh-corporate-events-error").textContent,
      ).toBe("no instrument is held"),
    );
    expect(screen.getByTestId("refresh-corporate-events-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
