import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  HoldingSchema,
  HoldingService,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
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
  holdings: [
    create(HoldingSchema, {
      instrumentId: "i-vusa",
      assetClass: AssetClass.SECURITY,
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.BROKER_DESCRIPTION,
          domain: "fidelity_uk/upload",
          value: "VANGUARD S&P 500 (VUSA)",
        }),
      ],
      quantity: "-141",
    }),
    create(HoldingSchema, {
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
    create(HoldingSchema, {
      instrumentId: "i-bae",
      assetClass: AssetClass.SECURITY,
      identifiers: [
        create(IdentifierSchema, {
          type: IdentifierType.BROKER_DESCRIPTION,
          domain: "fidelity_uk/upload",
          value: "BAE SYSTEMS (BA.)",
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
      "holding-row-i-bae",
      "holding-row-i-vusa",
    ]);
    const gbp = screen.getByTestId("holding-row-i-gbp");
    expect(gbp.textContent).toContain("GBP");
    expect(gbp.textContent).toContain("Cash");
    expect(screen.getByTestId("holding-qty-i-gbp").textContent).toBe(
      "12092.79",
    );
    const vusa = screen.getByTestId("holding-row-i-vusa");
    expect(vusa.textContent).toContain("VANGUARD S&P 500 (VUSA)");
    expect(vusa.textContent).toContain("Security");
    expect(screen.getByTestId("holding-qty-i-vusa").textContent).toBe(
      "-141.00",
    );
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
