import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  InstrumentSchema,
  InstrumentService,
  ListingSchema,
  ListInstrumentsResponseSchema,
} from "@/gen/instrument/v1/instrument_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { UploadProvider } from "@/contexts/upload-context";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import InstrumentsPage from "./page";

const page = (
  <UploadProvider>
    <InstrumentsPage />
  </UploadProvider>
);

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

const live = liveSession();

function serving(
  listInstruments: ServiceImpl<typeof InstrumentService>["listInstruments"],
) {
  return transportWith(live, ({ service }) => {
    service(InstrumentService, { listInstruments });
  });
}

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

const two = create(ListInstrumentsResponseSchema, {
  instruments: [
    create(InstrumentSchema, {
      id: "i-acme",
      assetClass: AssetClass.STOCK,
      identifiers: [ident(IdentifierType.ISIN, "US0378331005")],
      listings: [
        create(ListingSchema, {
          id: "l-usd",
          currency: "USD",
          identifiers: [
            ident(IdentifierType.MIC_TICKER, "ACME", "XNAS"),
            ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
          ],
        }),
        create(ListingSchema, {
          id: "l-gbp",
          currency: "GBP",
          identifiers: [ident(IdentifierType.MIC_TICKER, "ACME", "XLON")],
        }),
      ],
    }),
    create(InstrumentSchema, {
      id: "i-gbp",
      assetClass: AssetClass.CASH,
      identifiers: [ident(IdentifierType.CURRENCY, "GBP")],
      listings: [create(ListingSchema, { id: "l-cash", currency: "GBP" })],
    }),
  ],
});

describe("InstrumentsPage", () => {
  it("shows skeleton rows while loading", () => {
    renderWithAuth(
      page,
      serving(() => new Promise(() => {})),
    );
    expect(screen.getByTestId("instruments-table")).toBeTruthy();
    expect(screen.getByTestId("skeleton-rows")).toBeTruthy();
  });

  it("shows the empty state with an upload action without instruments", async () => {
    renderWithAuth(
      page,
      serving(() => create(ListInstrumentsResponseSchema, {})),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.getByTestId("upload-statement-empty")).toBeTruthy();
    expect(screen.queryByTestId("instruments-table")).toBeNull();
  });

  it("lists the instruments cash first with their identifiers and listings", async () => {
    renderWithAuth(
      page,
      serving(() => two),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instrument-row-i-gbp")).toBeTruthy(),
    );
    const rows = screen.getAllByTestId(/^instrument-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "instrument-row-i-gbp",
      "instrument-row-i-acme",
    ]);
    const acme = screen.getByTestId("instrument-row-i-acme");
    expect(acme.textContent).toContain("ACME");
    expect(acme.textContent).toContain("US0378331005");
    expect(acme.textContent).toContain("Stock");
    const usd = screen.getByTestId("listing-l-usd");
    expect(usd.textContent).toContain("USD");
    expect(usd.textContent).toContain("XNAS");
    expect(usd.textContent).toContain("BBG000B9XRY4");
    expect(screen.getByTestId("listing-l-gbp").textContent).toContain("XLON");
    const gbp = screen.getByTestId("instrument-row-i-gbp");
    expect(gbp.textContent).toContain("GBP");
    expect(gbp.textContent).toContain("Cash");
  });

  it("offers a retry when the list fails", async () => {
    let calls = 0;
    renderWithAuth(
      page,
      serving(() => {
        if (++calls === 1) {
          throw new ConnectError("down", Code.Unavailable);
        }
        return two;
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByTestId("instrument-row-i-gbp")).toBeTruthy(),
    );
  });
});
