import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  CoverageService,
  GetTickerResponseSchema,
  ListInstrTickersResponseSchema,
  TickerSchema,
  TickerValidity,
  WithdrawIdentifierAttestationResponseSchema,
} from "@/gen/admin/coverage/v1/coverage_pb";
import {
  InstrSchema,
  InstrService,
  ListingSchema,
  Listing_TickerSchema,
  ListInstrsResponseSchema,
} from "@/gen/admin/instr/v1/instr_pb";
import { AssertionItemSchema } from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
  TickerRelation,
} from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import InstrsPage from "./page";

const router = { push: vi.fn(), replace: vi.fn() };
let params = new URLSearchParams();
beforeEach(() => router.replace.mockClear());

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useSearchParams: () => params,
}));

const admin = liveSession({ role: Role.ADMIN });
type Coverage = Partial<ServiceImpl<typeof CoverageService>>;

const acme = { market: "US", value: "ACME" };
// ACME's tickers: the one that names it at XNAS, one that now names another
// instrument, and one that names nothing now.
const tickers = create(ListInstrTickersResponseSchema, {
  tickers: [
    {
      ticker: acme,
      relation: TickerRelation.CURRENT,
      txns: 3,
      venues: ["XNAS"],
    },
    { ticker: { ...acme, value: "GONE" }, relation: TickerRelation.DEFUNCT },
    { ticker: { ...acme, value: "OLD" }, relation: TickerRelation.FORMER },
  ],
});
// ACME's facts: a gap before a confirmed bracket, a datasource's assertion
// with an edge at its start and an identifier attestation, and one
// datasource's assertion coverage.
const read = create(GetTickerResponseSchema, {
  ticker: create(TickerSchema, {
    ticker: acme,
    txnDays: ["2024-02-01"],
    assertionCoverage: [
      {
        datasource: "alpha",
        coveredFrom: "2024-06-01",
        coveredBefore: "2026-01-01",
      },
    ],
    assertions: [
      create(AssertionItemSchema, {
        id: "a1",
        ticker: acme,
        assertedFrom: "2024-06-01",
        assertedBefore: "2026-01-01",
        edgeFrom: true,
        instrId: "i-acme",
        source: { case: "datasource", value: "alpha" },
      }),
      create(AssertionItemSchema, {
        id: "a2",
        ticker: acme,
        assertedFrom: "2026-01-01",
        assertedBefore: "2026-01-02",
        source: { case: "attestedBy", value: "admin@example.com" },
      }),
    ],
    intervals: [
      {
        validBefore: "2024-06-01",
        instrId: "i-acme",
        validity: TickerValidity.PROVISIONAL,
      },
      {
        validFrom: "2024-06-01",
        validBefore: "2026-01-01",
        instrId: "i-acme",
        validity: TickerValidity.CONFIRMED,
      },
    ],
    gaps: [
      {
        unconfirmedFrom: "2024-02-01",
        unconfirmedBefore: "2024-06-01",
        keys: 2,
        txns: 3,
      },
    ],
    instrs: [
      {
        id: "i-acme",
        name: { type: IdentifierType.ISIN, value: "US0000000001" },
      },
    ],
  }),
});

function serving(
  listInstrs: ServiceImpl<typeof InstrService>["listInstrs"],
  coverage: Coverage = {},
) {
  return transportWith(admin, ({ service }) => {
    service(InstrService, { listInstrs });
    service(CoverageService, {
      listInstrTickers: () => tickers,
      getTicker: () => read,
      ...coverage,
    });
  });
}

function tickerOn(
  market: string,
  value: string,
  mic: string,
  relation: TickerRelation,
) {
  return create(Listing_TickerSchema, {
    ticker: { market, value },
    mic,
    relation,
  });
}

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

// The dollar cash instrument, then a share with a dollar listing at two
// venues and a pound listing.
const listed = create(ListInstrsResponseSchema, {
  instrs: [
    create(InstrSchema, {
      id: "i-usd",
      name: ident(IdentifierType.CURRENCY, "USD"),
      assetClass: AssetClass.CASH,
      identifiers: [ident(IdentifierType.CURRENCY, "USD")],
      listings: [create(ListingSchema, { id: "l-cash", currency: "USD" })],
    }),
    create(InstrSchema, {
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
            ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
          ],
          tickers: [
            tickerOn("US", "ACME", "XNAS", TickerRelation.CURRENT),
            tickerOn("US", "ACME", "XNYS", TickerRelation.CURRENT),
          ],
        }),
        create(ListingSchema, {
          id: "l-gbp",
          currency: "GBP",
          tickers: [tickerOn("XLON", "ACM", "XLON", TickerRelation.DEFUNCT)],
        }),
      ],
      via: [ident(IdentifierType.BROKER_ID, "77", "ibkr")],
    }),
  ],
  nextPageToken: "i-acme",
});

describe("InstrsPage", () => {
  beforeEach(() => {
    params = new URLSearchParams();
  });

  it("shows skeleton rows while loading", () => {
    renderWithAuth(
      <InstrsPage />,
      serving(() => new Promise(() => {})),
    );
    expect(screen.getByTestId("skeleton-rows")).toBeTruthy();
  });

  it("shows the empty state when there are none", async () => {
    renderWithAuth(
      <InstrsPage />,
      serving(() => create(ListInstrsResponseSchema, {})),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.queryByTestId("admin-instrs-table")).toBeNull();
  });

  it("lists names and classes alone, and the next page", async () => {
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instr-row-i-acme")).toBeTruthy(),
    );
    const rows = screen.getAllByTestId(/^instr-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "instr-row-i-usd",
      "instr-row-i-acme",
    ]);
    const acme = screen.getByTestId("instr-row-i-acme");
    expect(acme.textContent).toContain("ACME");
    expect(acme.textContent).toContain("Stock");
    expect(acme.textContent).not.toContain("US0000000001");
    expect(acme.textContent).not.toContain("XNAS");
    expect(acme.getAttribute("aria-selected")).toBe("false");
    expect(screen.getByText("Select an instrument.")).toBeTruthy();
    expect(screen.queryByTestId("instr-pane")).toBeNull();
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin/instrs?after=i-acme",
    );
    expect(screen.getByTestId("list-next").textContent).toBe("Next");
  });

  it("shows the selected instrument in the pane by section", async () => {
    params = new URLSearchParams("after=i-prev&id=i-acme");
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed),
    );
    await waitFor(() => expect(screen.getByTestId("instr-pane")).toBeTruthy());
    expect(
      screen.getByTestId("instr-row-i-acme").getAttribute("aria-selected"),
    ).toBe("true");
    const ids = screen.getByTestId("instr-identifiers");
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
      screen.getByTestId("instr-pane").querySelectorAll("[data-name='true']"),
    ).toHaveLength(1);
    const gbp = screen.getByTestId("listing-l-gbp");
    expect(gbp.textContent).toContain("XLON");
    expect(gbp.textContent).toContain("ACM (defunct)");
    expect(usd.textContent).not.toContain("(defunct)");
    expect(
      screen.getByTestId("ticker-validity-XLON-ACM").getAttribute("href"),
    ).toBe(
      "/admin/instrs?after=i-prev&id=i-acme&tab=tickers&ticker=XLON%3AACM",
    );
    const overview = screen.getByTestId("instr-tab-overview");
    expect(overview.getAttribute("aria-selected")).toBe("true");
    expect(screen.getByTestId("instr-tab-tickers").getAttribute("href")).toBe(
      "/admin/instrs?after=i-prev&id=i-acme&tab=tickers",
    );
    expect(screen.queryByTestId("tickers-tab")).toBeNull();
    expect(screen.getByTestId("list-first").getAttribute("href")).toBe(
      "/admin/instrs",
    );
  });

  it("shows the first ticker's validity on the tickers tab and follows the choice", async () => {
    params = new URLSearchParams("id=i-acme&tab=tickers");
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("ticker-assertions")).toBeTruthy(),
    );
    expect(
      screen.getByTestId("instr-tab-tickers").getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.queryByTestId("instr-identifiers")).toBeNull();
    const select = screen.getByTestId("ticker-select") as HTMLSelectElement;
    expect(select.value).toBe("US:ACME");
    expect([...select.options].map((o) => o.textContent)).toEqual([
      "ACME in US",
      "GONE in US (defunct)",
      "OLD in US (former)",
    ]);
    expect(screen.getByTestId("coverage-timeline")).toBeTruthy();
    const gap = screen.getByTestId("pane-gap-2024-02-01");
    expect(gap.textContent).toContain("US0000000001");
    expect(gap.textContent).toContain("3");
    const a1 = screen.getByTestId("assertion-row-a1");
    expect(a1.textContent).toContain("US0000000001");
    expect(a1.textContent).toContain("alpha");
    expect(a1.textContent).toContain("2024-06-01");
    expect(a1.getAttribute("data-attested")).toBeNull();
    const a2 = screen.getByTestId("assertion-row-a2");
    expect(a2.textContent).toContain("nothing");
    expect(a2.textContent).toContain("2026-01-02");
    expect(a2.textContent).toContain("admin@example.com");
    expect(a2.getAttribute("data-attested")).toBe("true");
    expect(
      screen.getByTestId("ticker-assertion-coverage").textContent,
    ).toContain("2024-06-01");
    fireEvent.change(select, { target: { value: "US:OLD" } });
    expect(router.replace).toHaveBeenCalledWith(
      "/admin/instrs?id=i-acme&tab=tickers&ticker=US%3AOLD",
    );
  });

  it("shows the ticker the address names, and says when it has no facts", async () => {
    params = new URLSearchParams("id=i-acme&tab=tickers&ticker=US:OLD");
    const asked: string[] = [];
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed, {
        getTicker: (req) => {
          asked.push(req.ticker?.value ?? "");
          return create(GetTickerResponseSchema, {
            ticker: create(TickerSchema, { ticker: req.ticker }),
          });
        },
      }),
    );
    await waitFor(() => expect(screen.getByTestId("no-facts")).toBeTruthy());
    expect(asked).toEqual(["OLD"]);
    expect(
      (screen.getByTestId("ticker-select") as HTMLSelectElement).value,
    ).toBe("US:OLD");
    expect(screen.queryByTestId("coverage-timeline")).toBeNull();
  });

  it("shows the market's ticker for an address that names a venue of it", async () => {
    params = new URLSearchParams("id=i-acme&tab=tickers&ticker=XNYS:OLD");
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed, {
        listInstrTickers: () =>
          create(ListInstrTickersResponseSchema, {
            tickers: [
              {
                ticker: acme,
                relation: TickerRelation.CURRENT,
                venues: ["XNAS"],
              },
              {
                ticker: { ...acme, value: "OLD" },
                relation: TickerRelation.CURRENT,
                venues: ["XNYS"],
              },
            ],
          }),
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("ticker-select")).toBeTruthy(),
    );
    expect(
      (screen.getByTestId("ticker-select") as HTMLSelectElement).value,
    ).toBe("US:OLD");
  });

  it("says when no ticker names the instrument", async () => {
    params = new URLSearchParams("id=i-usd&tab=tickers");
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed, {
        listInstrTickers: () => create(ListInstrTickersResponseSchema, {}),
      }),
    );
    await waitFor(() => expect(screen.getByTestId("no-tickers")).toBeTruthy());
    expect(screen.queryByTestId("ticker-select")).toBeNull();
  });

  it("attests from a gap and from today, and withdraws an identifier attestation", async () => {
    params = new URLSearchParams("id=i-acme&tab=tickers");
    const withdrawn: string[] = [];
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed, {
        withdrawIdentifierAttestation: (req) => {
          withdrawn.push(req.assertionId);
          return create(WithdrawIdentifierAttestationResponseSchema, {});
        },
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("pane-gap-2024-02-01")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("gap-attest-2024-02-01"));
    expect((screen.getByTestId("attest-from") as HTMLInputElement).value).toBe(
      "2024-02-01",
    );
    fireEvent.click(screen.getByTestId("attest-dialog-close"));
    expect(screen.queryByTestId("attest-dialog")).toBeNull();

    fireEvent.click(screen.getByTestId("ticker-attest-today"));
    const from = screen.getByTestId("attest-from") as HTMLInputElement;
    expect(from.value).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    fireEvent.click(screen.getByTestId("attest-dialog-close"));

    expect(screen.queryByTestId("assertion-withdraw-a1")).toBeNull();
    fireEvent.click(screen.getByTestId("assertion-withdraw-a2"));
    expect(screen.getByTestId("withdraw-dialog").textContent).toContain(
      "2026-01-01",
    );
    fireEvent.click(screen.getByTestId("withdraw-confirm"));
    await waitFor(() =>
      expect(screen.queryByTestId("withdraw-dialog")).toBeNull(),
    );
    expect(withdrawn).toEqual(["a2"]);
  });

  it("follows the search box and the class filter in the address", async () => {
    params = new URLSearchParams("after=i-prev&id=i-acme&class=stock");
    renderWithAuth(
      <InstrsPage />,
      serving(() => listed),
    );
    await waitFor(() =>
      expect(screen.getByTestId("instr-row-i-acme")).toBeTruthy(),
    );
    const cls = screen.getByTestId("instrs-filter-class");
    expect((cls as HTMLSelectElement).value).toBe("stock");
    fireEvent.change(cls, { target: { value: "cash" } });
    expect(router.replace).toHaveBeenCalledWith("/admin/instrs?class=cash");
    fireEvent.change(screen.getByTestId("instrs-search"), {
      target: { value: "ms" },
    });
    await waitFor(() =>
      expect(router.replace).toHaveBeenCalledWith(
        "/admin/instrs?q=ms&class=stock",
      ),
    );
    expect(screen.getByTestId("list-first").getAttribute("href")).toBe(
      "/admin/instrs?class=stock",
    );
    expect(screen.getByTestId("list-next").getAttribute("href")).toBe(
      "/admin/instrs?after=i-acme&class=stock",
    );
  });

  it("retries after a failure", async () => {
    let calls = 0;
    renderWithAuth(
      <InstrsPage />,
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
      expect(screen.getByTestId("instr-row-i-usd")).toBeTruthy(),
    );
  });
});
