import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  GroupHoldingSchema,
  HoldingKeySchema,
  HoldingListingSchema,
  HoldingService,
  InstrumentHoldingSchema,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  Broker,
  IdentifierSchema,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { UploadProvider } from "@/contexts/upload-context";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import HoldingsPage from "./page";

const page = (
  <UploadProvider>
    <HoldingsPage />
  </UploadProvider>
);

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

const live = liveSession();

function serving(
  listHoldings: ServiceImpl<typeof HoldingService>["listHoldings"],
) {
  return transportWith(live, ({ service }) => {
    service(HoldingService, { listHoldings });
  });
}

const three = create(ListHoldingsResponseSchema, {
  instruments: [
    create(InstrumentHoldingSchema, {
      instrumentId: "i-vusa",
      assetClass: AssetClass.SECURITY,
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.ISIN,
          value: "IE00B3XXRP09",
        }),
      ],
      quantity: "-141",
    }),
    create(InstrumentHoldingSchema, {
      instrumentId: "i-acme",
      assetClass: AssetClass.STOCK,
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.MIC_TICKER,
          domain: "XNAS",
          value: "ACME",
        }),
        create(IdentifierSchema, {
          type: IdentifierType.MIC_TICKER,
          domain: "XNYS",
          value: "ACME",
        }),
        create(IdentifierSchema, {
          type: IdentifierType.ISIN,
          value: "US0378331005",
        }),
      ],
      quantity: "10",
      listings: [
        create(HoldingListingSchema, {
          id: "l-usd",
          currency: "USD",
          venue: "Nasdaq",
          ticker: create(IdentifierSchema, {
            type: IdentifierType.MIC_TICKER,
            domain: "XNAS",
            value: "ACME",
          }),
        }),
      ],
      keys: [
        create(HoldingKeySchema, {
          statedKeyId: "k-acme",
          statementId: "s-1",
          broker: Broker.IBKR,
          listingId: "l-usd",
          quantity: "10",
          statedKey: create(StatedKeySchema, {
            identifiers: [
              create(IdentifierSchema, {
                type: IdentifierType.BROKER_DESCRIPTION,
                domain: "ibkr",
                value: "ACME INC",
              }),
            ],
          }),
        }),
      ],
    }),
    create(InstrumentHoldingSchema, {
      instrumentId: "i-gbp",
      assetClass: AssetClass.CASH,
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.CURRENCY,
          value: "GBP",
        }),
      ],
      quantity: "12092.79",
    }),
  ],
  groups: [
    create(GroupHoldingSchema, {
      groupId: "g-bae",
      assetClasses: [AssetClass.SECURITY],
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.MIC_TICKER,
          value: "BA.",
        }),
        create(IdentifierSchema, {
          type: IdentifierType.BROKER_DESCRIPTION,
          domain: "fidelity_uk",
          value: "BAE SYSTEMS (BA.)",
        }),
        create(IdentifierSchema, {
          type: IdentifierType.BROKER_DESCRIPTION,
          domain: "ibkr",
          value: "BAE SYSTEMS PLC",
        }),
      ],
      quantity: "120",
    }),
  ],
});

describe("HoldingsPage", () => {
  it("shows skeleton rows while loading", () => {
    renderWithAuth(
      page,
      serving(() => new Promise(() => {})),
    );
    expect(screen.getByTestId("holdings-table")).toBeTruthy();
    expect(screen.getByTestId("skeleton-rows")).toBeTruthy();
  });

  it("shows the empty state with an upload action without holdings", async () => {
    renderWithAuth(
      page,
      serving(() => create(ListHoldingsResponseSchema, {})),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.getByTestId("upload-statement-empty")).toBeTruthy();
    expect(screen.queryByTestId("holdings-table")).toBeNull();
  });

  it("lists the holdings cash first with their label, class and quantity", async () => {
    renderWithAuth(
      page,
      serving(() => three),
    );
    await waitFor(() =>
      expect(screen.getByTestId("holding-row-i-gbp")).toBeTruthy(),
    );
    const rows = screen.getAllByTestId(/^holding-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "holding-row-i-gbp",
      "holding-row-i-acme",
      "holding-row-g-bae",
      "holding-row-i-vusa",
    ]);
    const gbp = screen.getByTestId("holding-row-i-gbp");
    expect(gbp.textContent).toContain("GBP");
    expect(gbp.textContent).toContain("Cash");
    expect(screen.getByTestId("holding-qty-i-gbp").textContent).toBe(
      "12092.79",
    );
    const vusa = screen.getByTestId("holding-row-i-vusa");
    expect(vusa.textContent).toContain("IE00B3XXRP09");
    expect(vusa.textContent).toContain("Security");
    expect(screen.getByTestId("holding-qty-i-vusa").textContent).toBe(
      "-141.00",
    );
    const bae = screen.getByTestId("holding-row-g-bae");
    expect(bae.textContent).toContain("BAE SYSTEMS (BA.)");
    expect(screen.getByTestId("holding-qty-g-bae").textContent).toBe("120.00");
  });

  it("marks a group as unidentified and shows what its keys state", async () => {
    renderWithAuth(
      page,
      serving(() => three),
    );
    await waitFor(() =>
      expect(screen.getByTestId("holding-row-g-bae")).toBeTruthy(),
    );
    const bae = screen.getByTestId("holding-row-g-bae");
    expect(bae.getAttribute("data-kind")).toBe("group");
    const basis = screen.getByTestId("holding-basis");
    expect(bae.contains(basis)).toBe(true);
    expect(basis.getAttribute("data-state")).toBe("unidentified");
    expect(basis.textContent).toBe("Unidentified");
    expect(bae.textContent).toContain("BA.");
    expect(bae.textContent).toContain("BAE SYSTEMS (BA.)");
    expect(bae.textContent).toContain("BAE SYSTEMS PLC");
    expect(bae.textContent).not.toContain("(ibkr)");
    expect(screen.getAllByTestId("holding-basis")).toHaveLength(1);
  });

  it("names a resolved holding by its ticker at its venue, with the description and one code", async () => {
    renderWithAuth(
      page,
      serving(() => three),
    );
    await waitFor(() =>
      expect(screen.getByTestId("holding-row-i-acme")).toBeTruthy(),
    );
    const acme = screen.getByTestId("holding-row-i-acme");
    expect(acme.getAttribute("data-kind")).toBe("instrument");
    expect(acme.textContent).toContain("ACME");
    expect(screen.getByTestId("holding-venue-i-acme").textContent).toBe(
      "Nasdaq",
    );
    expect(acme.textContent).toContain("ACME INC");
    expect(acme.textContent).toContain("US0378331005");
    expect(screen.getByTestId("holding-currency-i-acme").textContent).toBe(
      "USD",
    );
    expect(acme.textContent).not.toContain("XNYS");
    expect(acme.textContent).not.toContain("XNAS");
    expect(acme.textContent).not.toContain("Unidentified");
  });

  it("offers a retry when the list fails", async () => {
    let calls = 0;
    renderWithAuth(
      page,
      serving(() => {
        if (++calls === 1) {
          throw new ConnectError("down", Code.Unavailable);
        }
        return three;
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByTestId("holding-row-i-gbp")).toBeTruthy(),
    );
  });
});
