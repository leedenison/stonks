import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  InstrumentSchema,
  InstrumentService,
  ListingSchema,
  ListInstrumentsResponseSchema,
} from "@/gen/admin/instrument/v1/instrument_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import InstrumentsPage from "./page";

const router = { push: vi.fn(), replace: vi.fn() };
let params = new URLSearchParams();
beforeEach(() => router.replace.mockClear());

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useSearchParams: () => params,
}));

const admin = liveSession({ role: Role.ADMIN });

function serving(
  listInstruments: ServiceImpl<typeof InstrumentService>["listInstruments"],
) {
  return transportWith(admin, ({ service }) => {
    service(InstrumentService, { listInstruments });
  });
}

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

// The dollar cash instrument, then a share with a dollar listing at two
// venues and a pound listing.
const listed = create(ListInstrumentsResponseSchema, {
  instruments: [
    create(InstrumentSchema, {
      id: "i-usd",
      name: ident(IdentifierType.CURRENCY, "USD"),
      assetClass: AssetClass.CASH,
      identifiers: [ident(IdentifierType.CURRENCY, "USD")],
      listings: [create(ListingSchema, { id: "l-cash", currency: "USD" })],
    }),
    create(InstrumentSchema, {
      id: "i-acme",
      name: ident(IdentifierType.MIC_TICKER, "ACME", "XNAS"),
      assetClass: AssetClass.STOCK,
      identifiers: [
        ident(IdentifierType.ISIN, "US0000000001"),
        ident(IdentifierType.BROKER_ID, "77", "ibkr"),
      ],
      listings: [
        create(ListingSchema, {
          id: "l-usd",
          currency: "USD",
          primaryMic: "XNAS",
          identifiers: [
            ident(IdentifierType.MIC_TICKER, "ACME", "XNAS"),
            ident(IdentifierType.MIC_TICKER, "ACME", "XNYS"),
            ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
          ],
        }),
        create(ListingSchema, {
          id: "l-gbp",
          currency: "GBP",
          identifiers: [ident(IdentifierType.MIC_TICKER, "ACM", "XLON")],
        }),
      ],
      via: [ident(IdentifierType.BROKER_ID, "77", "ibkr")],
    }),
  ],
  nextPageToken: "i-acme",
});

describe("InstrumentsPage", () => {
  beforeEach(() => {
    params = new URLSearchParams();
  });

  it("shows skeleton rows while loading", () => {
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => new Promise(() => {})),
    );
    expect(screen.getByTestId("skeleton-rows")).toBeTruthy();
  });

  it("shows the empty state when there are none", async () => {
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => create(ListInstrumentsResponseSchema, {})),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.queryByTestId("admin-instruments-table")).toBeNull();
  });

  it("lists names and classes alone, and the next page", async () => {
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instrument-row-i-acme")).toBeTruthy(),
    );
    const rows = screen.getAllByTestId(/^instrument-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "instrument-row-i-usd",
      "instrument-row-i-acme",
    ]);
    const acme = screen.getByTestId("instrument-row-i-acme");
    expect(acme.textContent).toContain("ACME");
    expect(acme.textContent).toContain("Stock");
    expect(acme.textContent).not.toContain("US0000000001");
    expect(acme.textContent).not.toContain("XNAS");
    expect(acme.getAttribute("aria-selected")).toBe("false");
    expect(screen.getByText("Select an instrument.")).toBeTruthy();
    expect(screen.queryByTestId("instrument-pane")).toBeNull();
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin/instruments?after=i-acme",
    );
    expect(screen.getByTestId("list-next").textContent).toBe("Next");
  });

  it("shows the selected instrument in the pane by section", async () => {
    params = new URLSearchParams("after=i-prev&id=i-acme");
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instrument-pane")).toBeTruthy(),
    );
    expect(
      screen.getByTestId("instrument-row-i-acme").getAttribute("aria-selected"),
    ).toBe("true");
    const ids = screen.getByTestId("instrument-identifiers");
    const isin = ids.querySelector("[data-identifier-type='ISIN']");
    expect(isin?.textContent).toContain("ISIN");
    expect(isin?.textContent).toContain("US0000000001");
    expect(
      ids.querySelector("[data-identifier-type='BROKER_ID']")?.textContent,
    ).toContain("ibkr");
    // The via broker id is marked but does not move ahead of the ISIN,
    // because type rank comes first.
    const rows = ids.querySelectorAll("tbody tr");
    expect(rows[0].getAttribute("data-identifier-type")).toBe("ISIN");
    expect(rows[0].getAttribute("data-via")).toBeNull();
    expect(rows[1].getAttribute("data-identifier-type")).toBe("BROKER_ID");
    expect(rows[1].getAttribute("data-via")).toBe("true");
    const usd = screen.getByTestId("listing-l-usd");
    expect(usd.textContent).toContain("USD");
    expect(usd.textContent).toContain("Primary");
    expect(usd.textContent).toContain("XNAS");
    const tickers = usd.querySelectorAll("[data-identifier-type='MIC_TICKER']");
    expect(tickers).toHaveLength(1);
    expect(tickers[0].textContent).toContain("XNAS");
    expect(tickers[0].textContent).toContain("XNYS");
    expect(tickers[0].textContent).not.toContain(",");
    const primary = tickers[0].querySelectorAll("[data-primary='true']");
    expect(primary).toHaveLength(1);
    expect(primary[0].textContent).toBe("XNAS");
    // The ticker names the instrument, so its row alone is marked.
    expect(tickers[0].getAttribute("data-name")).toBe("true");
    expect(
      screen
        .getByTestId("instrument-pane")
        .querySelectorAll("[data-name='true']"),
    ).toHaveLength(1);
    expect(screen.getByTestId("listing-l-gbp").textContent).toContain("XLON");
    expect(
      screen.getByTestId("ticker-coverage-XLON-ACM").getAttribute("href"),
    ).toBe("/admin/coverage?domain=XLON&value=ACM");
    expect(screen.getByTestId("list-first").getAttribute("href")).toBe(
      "/admin/instruments",
    );
  });

  it("follows the search box and the class filter in the address", async () => {
    params = new URLSearchParams("after=i-prev&id=i-acme&class=stock");
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instrument-row-i-acme")).toBeTruthy(),
    );
    const cls = screen.getByTestId("instruments-filter-class");
    expect((cls as HTMLSelectElement).value).toBe("stock");
    fireEvent.change(cls, { target: { value: "cash" } });
    expect(router.replace).toHaveBeenCalledWith(
      "/admin/instruments?class=cash",
    );
    fireEvent.change(screen.getByTestId("instruments-search"), {
      target: { value: "ms" },
    });
    await waitFor(() =>
      expect(router.replace).toHaveBeenCalledWith(
        "/admin/instruments?q=ms&class=stock",
      ),
    );
    expect(screen.getByTestId("list-first").getAttribute("href")).toBe(
      "/admin/instruments?class=stock",
    );
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin/instruments?after=i-acme&class=stock",
    );
  });

  it("retries after a failure", async () => {
    let calls = 0;
    renderWithAuth(
      <InstrumentsPage />,
      serving(() => {
        calls += 1;
        if (calls === 1) {
          throw new ConnectError("down", Code.Unavailable);
        }
        return listed;
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByTestId("instrument-row-i-usd")).toBeTruthy(),
    );
  });
});
