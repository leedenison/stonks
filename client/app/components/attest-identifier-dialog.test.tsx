import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  type AttestIdentifierRequest,
  AttestIdentifierResponseSchema,
  CoverageService,
  GetTickerResponseSchema,
  TickerSchema,
  TickerValidity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import {
  InstrumentSchema,
  InstrumentService,
  ListInstrumentsResponseSchema,
} from "@/gen/admin/instrument/v1/instrument_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunSchema } from "@/gen/run/v1/run_pb";
import {
  IdentifierSchema,
  IdentifierType,
  MarketTickerSchema,
} from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { AttestIdentifierDialog } from "./attest-identifier-dialog";

const admin = liveSession({ role: Role.ADMIN });
const acme = create(MarketTickerSchema, { market: "US", value: "ACME" });
const isin = create(IdentifierSchema, {
  type: IdentifierType.ISIN,
  value: "US0000000001",
});

// held is a ticker whose validity names i1 provisionally before 2024-06-01;
// bare is a ticker without facts.
const held = create(GetTickerResponseSchema, {
  ticker: create(TickerSchema, {
    ticker: acme,
    intervals: [
      {
        validBefore: "2024-06-01",
        instrumentId: "i1",
        validity: TickerValidity.PROVISIONAL,
      },
    ],
    instruments: [{ id: "i1", name: isin }],
  }),
});
const bare = create(GetTickerResponseSchema, {
  ticker: create(TickerSchema, { ticker: acme }),
});
const found = create(ListInstrumentsResponseSchema, {
  instruments: [
    create(InstrumentSchema, {
      id: "i2",
      name: { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "ACMX" },
    }),
  ],
});

// serving returns a transport and the identifier attestation requests it
// received. A refuse makes every identifier attestation fail with it.
function serving(ticker = held, refuse?: ConnectError) {
  const requests: AttestIdentifierRequest[] = [];
  const attestIdentifier: ServiceImpl<
    typeof CoverageService
  >["attestIdentifier"] = (req) => {
    if (refuse) throw refuse;
    requests.push(req);
    return create(AttestIdentifierResponseSchema, {
      run: create(RunSchema, { id: "r1" }),
    });
  };
  const transport = transportWith(admin, ({ service }) => {
    service(CoverageService, { getTicker: () => ticker, attestIdentifier });
    service(InstrumentService, { listInstruments: () => found });
  });
  return { transport, requests };
}

function dialog(
  transport: ReturnType<typeof serving>["transport"],
  onDone = vi.fn(),
) {
  renderWithAuth(
    <AttestIdentifierDialog
      ticker={acme}
      from="2024-02-01"
      onClose={vi.fn()}
      onDone={onDone}
    />,
    transport,
  );
  return onDone;
}

describe("AttestIdentifierDialog", () => {
  it("preselects the instrument holding the day and attests a range", async () => {
    const { transport, requests } = serving();
    const onDone = dialog(transport);
    const start = screen.getByTestId("attest-start");
    await waitFor(() => expect(start.hasAttribute("disabled")).toBe(false));
    expect(screen.getByTestId("attest-dialog").textContent).toContain(
      "US0000000001",
    );
    expect((screen.getByTestId("attest-from") as HTMLInputElement).value).toBe(
      "2024-02-01",
    );
    expect(
      (screen.getByTestId("attest-names") as HTMLInputElement).checked,
    ).toBe(true);
    fireEvent.change(screen.getByTestId("attest-to"), {
      target: { value: "2024-03-01" },
    });
    fireEvent.click(start);
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(requests).toHaveLength(1);
    expect(requests[0].ticker?.value).toBe("ACME");
    expect(requests[0].names).toEqual({ case: "instrumentId", value: "i1" });
    expect(requests[0].from).toBe("2024-02-01");
    expect(requests[0].to).toBe("2024-03-01");
  });

  it("attests nothing where the ticker names nothing on the day", async () => {
    const { transport, requests } = serving(bare);
    dialog(transport);
    await waitFor(() =>
      expect(
        (screen.getByTestId("attest-nothing") as HTMLInputElement).checked,
      ).toBe(true),
    );
    fireEvent.click(screen.getByTestId("attest-start"));
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].names).toEqual({ case: "nothing", value: true });
    expect(requests[0].to).toBeUndefined();
  });

  it("searches the instruments and attests the one picked", async () => {
    const { transport, requests } = serving(bare);
    dialog(transport);
    await waitFor(() =>
      expect(
        (screen.getByTestId("attest-nothing") as HTMLInputElement).checked,
      ).toBe(true),
    );
    fireEvent.click(screen.getByTestId("attest-names"));
    expect(screen.getByTestId("attest-start").hasAttribute("disabled")).toBe(
      true,
    );
    fireEvent.change(screen.getByTestId("attest-instrument-search"), {
      target: { value: "acm" },
    });
    await waitFor(() =>
      expect(
        screen.getByTestId("attest-instrument").querySelectorAll("option"),
      ).toHaveLength(1),
    );
    fireEvent.change(screen.getByTestId("attest-instrument"), {
      target: { value: "i2" },
    });
    expect(screen.getByTestId("attest-dialog").textContent).toContain("ACMX");
    fireEvent.click(screen.getByTestId("attest-start"));
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].names).toEqual({ case: "instrumentId", value: "i2" });
  });

  it("shows a refusal and stays open", async () => {
    const { transport } = serving(
      held,
      new ConnectError(
        "2026-12-01: an identifier attestation cannot reach past the day it is made",
        Code.FailedPrecondition,
      ),
    );
    const onDone = dialog(transport);
    const start = screen.getByTestId("attest-start");
    await waitFor(() => expect(start.hasAttribute("disabled")).toBe(false));
    fireEvent.click(start);
    await waitFor(() =>
      expect(screen.getByTestId("attest-error")).toBeTruthy(),
    );
    expect(screen.getByTestId("attest-error").textContent).toContain(
      "cannot reach past",
    );
    expect(screen.getByTestId("attest-dialog")).toBeTruthy();
    expect(onDone).not.toHaveBeenCalled();
  });
});
