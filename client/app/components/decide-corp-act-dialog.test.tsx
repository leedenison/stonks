import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  CorpActDecision,
  CorpActSchema,
  CorpActService,
  DecideCorpActResponseSchema,
  type GetCorpActResponse,
  GetCorpActResponseSchema,
} from "@/gen/admin/corpact/v1/corpact_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { DecideCorpActDialog } from "./decide-corp-act-dialog";

const admin = liveSession({ role: Role.ADMIN });

// ge is GE's spin-off as its datasource states it, which the system stored
// unhandled.
const ge = {
  id: "a1",
  ticker: { market: "US", value: "GE" },
  exDate: "2024-04-02",
  ratio: { from: "1000", to: "1253" },
  datasource: "alpha",
  type: "forward_split",
  applied: true,
};

// spun is ge with a statement split and another datasource's action nearby.
const spun = create(GetCorpActResponseSchema, {
  corpAct: create(CorpActSchema, ge),
  stmtSplits: [
    { effectiveDate: "2024-04-01", ratio: { from: "1", to: "2" } },
    { effectiveDate: "2024-04-03" },
  ],
  otherCorpActs: [
    create(CorpActSchema, {
      id: "a2",
      ticker: { market: "US", value: "GE" },
      exDate: "2024-04-09",
      ratio: { from: "1", to: "2" },
      datasource: "beta",
    }),
  ],
});

function serving(
  read: GetCorpActResponse,
  impl: Partial<ServiceImpl<typeof CorpActService>> = {},
) {
  return transportWith(admin, ({ service }) => {
    service(CorpActService, { getCorpAct: () => read, ...impl });
  });
}

describe("DecideCorpActDialog", () => {
  it("shows the evidence and saves the chosen type", async () => {
    const decideCorpAct = vi.fn(() => create(DecideCorpActResponseSchema, {}));
    const onClose = vi.fn();
    renderWithAuth(
      <DecideCorpActDialog corpActId="a1" onClose={onClose} />,
      serving(spun, { decideCorpAct }),
    );
    const evidence = await screen.findByTestId("decide-evidence");
    expect(evidence.textContent).toContain("alpha states forward_split");
    expect(evidence.textContent).toContain("2024-04-01, 2 for 1");
    expect(evidence.textContent).toContain("2024-04-03");
    expect(evidence.textContent).toContain("beta: 2024-04-09, 2 for 1");
    expect(screen.getByTestId("decide-dialog").textContent).toContain(
      "1253 for 1000",
    );

    const save = screen.getByTestId("decide-save") as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByTestId("decide-type"), {
      target: { value: String(CorpActDecision.SPIN_OFF_PRICE_FACTOR) },
    });
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() =>
      expect(decideCorpAct).toHaveBeenCalledWith(
        expect.objectContaining({
          corpActId: "a1",
          decision: CorpActDecision.SPIN_OFF_PRICE_FACTOR,
        }),
        expect.anything(),
      ),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("starts from the recorded decision", async () => {
    const decided = create(GetCorpActResponseSchema, {
      ...spun,
      corpAct: create(CorpActSchema, {
        ...ge,
        decision: CorpActDecision.SPLIT,
        restatesShares: true,
      }),
    });
    renderWithAuth(
      <DecideCorpActDialog corpActId="a1" onClose={() => {}} />,
      serving(decided),
    );
    const select = (await screen.findByTestId(
      "decide-type",
    )) as HTMLSelectElement;
    expect(select.value).toBe(String(CorpActDecision.SPLIT));
    expect(
      (screen.getByTestId("decide-save") as HTMLButtonElement).disabled,
    ).toBe(false);
  });

  it("says when the evidence is empty", async () => {
    const bare = create(GetCorpActResponseSchema, {
      corpAct: create(CorpActSchema, { ...ge, type: undefined }),
    });
    renderWithAuth(
      <DecideCorpActDialog corpActId="a1" onClose={() => {}} />,
      serving(bare),
    );
    const evidence = await screen.findByTestId("decide-evidence");
    expect(evidence.textContent).toContain("alpha did not state a type");
    expect(evidence.textContent).toContain(
      "Statement splits within 7 daysNone",
    );
    expect(evidence.textContent).toContain(
      "Other datasources within 7 daysNone",
    );
  });

  it("keeps a refusal in the dialog", async () => {
    const decideCorpAct = vi.fn(() => {
      throw new ConnectError(
        "no such corporate action",
        Code.FailedPrecondition,
      );
    });
    renderWithAuth(
      <DecideCorpActDialog corpActId="a1" onClose={() => {}} />,
      serving(spun, { decideCorpAct }),
    );
    await screen.findByTestId("decide-evidence");
    fireEvent.change(screen.getByTestId("decide-type"), {
      target: { value: String(CorpActDecision.OTHER) },
    });
    fireEvent.click(screen.getByTestId("decide-save"));
    await waitFor(() =>
      expect(screen.getByTestId("decide-error").textContent).toContain(
        "no such corporate action",
      ),
    );
  });
});
