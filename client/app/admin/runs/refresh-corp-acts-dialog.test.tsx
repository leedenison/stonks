import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  RefreshCorpActsService,
  StartRefreshCorpActsResponseSchema,
} from "@/gen/admin/refresh_corp_acts/v1/refresh_corp_acts_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunSchema, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { RefreshCorpActsDialog } from "./refresh-corp-acts-dialog";

const started = create(RunSchema, {
  id: "x1",
  kind: RunKind.REFRESH_CORP_ACTS,
  trigger: RunTrigger.ADMINISTRATOR,
  state: RunState.PENDING,
});

function serving(
  startRefreshCorpActs = vi.fn(() =>
    create(StartRefreshCorpActsResponseSchema, { run: started }),
  ),
) {
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(RefreshCorpActsService, { startRefreshCorpActs });
  });
}

describe("RefreshCorpActsDialog", () => {
  it("refreshes corporate actions and hands over the run", async () => {
    const onStarted = vi.fn();
    renderWithAuth(
      <RefreshCorpActsDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(),
    );
    expect(
      screen.getByTestId("refresh-corp-acts-dialog").textContent,
    ).toContain("ticker that named a held instrument");
    fireEvent.click(screen.getByTestId("refresh-corp-acts-start"));
    await waitFor(() => expect(onStarted).toHaveBeenCalledWith(started));
  });

  it("shows a refusal and stays open", async () => {
    const onStarted = vi.fn();
    const startRefreshCorpActs = vi.fn(() => {
      throw new ConnectError("no instrument is held", Code.FailedPrecondition);
    });
    renderWithAuth(
      <RefreshCorpActsDialog onClose={vi.fn()} onStarted={onStarted} />,
      serving(startRefreshCorpActs),
    );
    fireEvent.click(screen.getByTestId("refresh-corp-acts-start"));
    await waitFor(() =>
      expect(screen.getByTestId("refresh-corp-acts-error").textContent).toBe(
        "no instrument is held",
      ),
    );
    expect(screen.getByTestId("refresh-corp-acts-dialog")).toBeTruthy();
    expect(onStarted).not.toHaveBeenCalled();
  });
});
