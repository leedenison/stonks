import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  CoverageService,
  GetTickerResponseSchema,
  WithdrawAttestationResponseSchema,
  ListDomainsResponseSchema,
  ListGapsResponseSchema,
  TickerSchema,
  Validity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import {
  InstrumentService,
  ListInstrumentsResponseSchema,
} from "@/gen/admin/instrument/v1/instrument_pb";
import { AssertionItemSchema } from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { IdentifierType } from "@/gen/type/v1/type_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import CoveragePage from "./page";

const router = { push: vi.fn(), replace: vi.fn() };
let params = new URLSearchParams();

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  useSearchParams: () => params,
}));

const admin = liveSession({ role: Role.ADMIN });
type Impl = Partial<ServiceImpl<typeof CoverageService>>;

const acme = { type: IdentifierType.MIC_TICKER, domain: "XNAS", value: "ACME" };
const beta = { ...acme, value: "BETA" };
const domains = create(ListDomainsResponseSchema, {
  domains: [
    { mic: "XLON", tickers: 1 },
    { mic: "XNAS", tickers: 2 },
  ],
});
const gaps = create(ListGapsResponseSchema, {
  gaps: [
    {
      ticker: acme,
      unconfirmedFrom: "2024-02-01",
      unconfirmedBefore: "2024-06-01",
      keys: 2,
      transactions: 3,
    },
    {
      ticker: beta,
      unconfirmedFrom: "2025-01-01",
      unconfirmedBefore: "2026-01-02",
      keys: 1,
      transactions: 1,
    },
  ],
});
const read = create(GetTickerResponseSchema, {
  ticker: create(TickerSchema, {
    ticker: acme,
    transactionDays: ["2024-02-01"],
    coverage: [
      {
        datasource: "alpha",
        covers: { ...acme, value: "" },
        coveredFrom: "2024-06-01",
        coveredBefore: "2026-01-01",
      },
    ],
    assertions: [
      create(AssertionItemSchema, {
        id: "a1",
        ticker: acme,
        assertedOn: "2024-06-01",
        instrumentId: "i1",
        source: { case: "datasource", value: "alpha" },
      }),
      create(AssertionItemSchema, {
        id: "a2",
        ticker: acme,
        assertedOn: "2026-01-01",
        source: { case: "attestedBy", value: "admin@example.com" },
      }),
    ],
    intervals: [
      {
        validBefore: "2024-06-01",
        instrumentId: "i1",
        validity: Validity.PROVISIONAL,
      },
      {
        validFrom: "2024-06-01",
        validBefore: "2026-01-01",
        instrumentId: "i1",
        validity: Validity.CONFIRMED,
      },
    ],
    gaps: [
      {
        ticker: acme,
        unconfirmedFrom: "2024-02-01",
        unconfirmedBefore: "2024-06-01",
        keys: 2,
        transactions: 3,
      },
    ],
    instruments: [
      { id: "i1", name: { type: IdentifierType.ISIN, value: "US0000000001" } },
    ],
  }),
});

function serving(impl: Impl) {
  return transportWith(admin, ({ service }) => {
    service(CoverageService, {
      listDomains: () => domains,
      listGaps: () => gaps,
      getTicker: () => read,
      ...impl,
    });
    service(InstrumentService, {
      listInstruments: () => create(ListInstrumentsResponseSchema, {}),
    });
  });
}

describe("CoveragePage", () => {
  beforeEach(() => {
    params = new URLSearchParams();
    router.replace.mockClear();
  });

  it("shows skeleton rows while loading", async () => {
    renderWithAuth(
      <CoveragePage />,
      serving({ listGaps: () => new Promise(() => {}) }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("skeleton-rows")).toBeTruthy(),
    );
  });

  it("shows the empty state when no key states a ticker", async () => {
    renderWithAuth(
      <CoveragePage />,
      serving({ listDomains: () => create(ListDomainsResponseSchema, {}) }),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.queryByTestId("coverage-gaps-table")).toBeNull();
  });

  it("lists the first domain's gaps in the order served", async () => {
    const asked: string[] = [];
    renderWithAuth(
      <CoveragePage />,
      serving({
        listGaps: (req) => {
          asked.push(req.domain);
          return gaps;
        },
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("gap-row-ACME-2024-02-01")).toBeTruthy(),
    );
    expect(asked).toEqual(["XLON"]);
    const rows = screen.getAllByTestId(/^gap-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "gap-row-ACME-2024-02-01",
      "gap-row-BETA-2025-01-01",
    ]);
    const row = screen.getByTestId("gap-row-ACME-2024-02-01");
    expect(row.textContent).toContain("ACME");
    expect(row.textContent).toContain("2024-02-01");
    expect(row.textContent).toContain("2024-06-01");
    expect(row.getAttribute("aria-selected")).toBe("false");
    expect(row.querySelector("a")?.getAttribute("href")).toBe(
      "/admin/coverage?domain=XLON&value=ACME",
    );
    expect(
      (screen.getByTestId("coverage-filter-domain") as HTMLSelectElement).value,
    ).toBe("XLON");
    expect(screen.getByText("Select a gap.")).toBeTruthy();
    expect(screen.queryByTestId("ticker-pane")).toBeNull();
  });

  it("follows the domain in the address and changes it", async () => {
    params = new URLSearchParams("domain=XNAS&value=ACME");
    renderWithAuth(<CoveragePage />, serving({}));
    await waitFor(() =>
      expect(screen.getByTestId("gap-row-ACME-2024-02-01")).toBeTruthy(),
    );
    const select = screen.getByTestId("coverage-filter-domain");
    expect((select as HTMLSelectElement).value).toBe("XNAS");
    fireEvent.change(select, { target: { value: "XLON" } });
    expect(router.replace).toHaveBeenCalledWith("/admin/coverage?domain=XLON");
  });

  it("shows the selected ticker in the pane by section", async () => {
    params = new URLSearchParams("domain=XNAS&value=ACME");
    renderWithAuth(<CoveragePage />, serving({}));
    await waitFor(() =>
      expect(screen.getByTestId("ticker-assertions")).toBeTruthy(),
    );
    expect(
      screen
        .getByTestId("gap-row-ACME-2024-02-01")
        .getAttribute("aria-selected"),
    ).toBe("true");
    expect(screen.getByTestId("coverage-timeline")).toBeTruthy();
    const gap = screen.getByTestId("pane-gap-2024-02-01");
    expect(gap.textContent).toContain("US0000000001");
    expect(gap.textContent).toContain("3");
    const a1 = screen.getByTestId("assertion-row-a1");
    expect(a1.textContent).toContain("US0000000001");
    expect(a1.textContent).toContain("alpha");
    expect(a1.getAttribute("data-attested")).toBeNull();
    const a2 = screen.getByTestId("assertion-row-a2");
    expect(a2.textContent).toContain("nothing");
    expect(a2.textContent).toContain("admin@example.com");
    expect(a2.getAttribute("data-attested")).toBe("true");
    expect(screen.getByTestId("ticker-coverage").textContent).toContain(
      "every ticker of XNAS",
    );
    expect(screen.queryByTestId("ticker-events")).toBeNull();
  });

  it("attests from a gap, from the action bar, and withdraws an attestation", async () => {
    params = new URLSearchParams("domain=XNAS&value=ACME");
    const withdrawn: string[] = [];
    const withdrawAttestation: ServiceImpl<
      typeof CoverageService
    >["withdrawAttestation"] = (req) => {
      withdrawn.push(req.assertionId);
      return create(WithdrawAttestationResponseSchema, {});
    };
    renderWithAuth(<CoveragePage />, serving({ withdrawAttestation }));
    await waitFor(() =>
      expect(screen.getByTestId("pane-gap-2024-02-01")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("gap-attest-2024-02-01"));
    expect((screen.getByTestId("attest-from") as HTMLInputElement).value).toBe(
      "2024-02-01",
    );
    fireEvent.click(screen.getByTestId("attest-dialog-close"));
    expect(screen.queryByTestId("attest-dialog")).toBeNull();

    fireEvent.click(screen.getByTestId("coverage-attest"));
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

  it("retries after a failure", async () => {
    let calls = 0;
    renderWithAuth(
      <CoveragePage />,
      serving({
        listGaps: () => {
          calls += 1;
          if (calls === 1) {
            throw new ConnectError("down", Code.Unavailable);
          }
          return gaps;
        },
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByTestId("gap-row-ACME-2024-02-01")).toBeTruthy(),
    );
  });
});
