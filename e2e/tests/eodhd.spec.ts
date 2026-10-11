import path from "node:path";
import { FetchOutcome } from "../gen/admin/run/v1/run_pb";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  adminRunClient,
  fetchItems,
  fetchRun,
  holdingClient,
  adminInstrumentClient,
  expectServedOnce,
  resolutionItems,
} from "../helpers/api";
import { uploadStatement } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// One IBKR line, derived from the client's IBKR test export, which is
// modelled on a real export with its identifiers replaced. It states the ISIN
// of Procter & Gamble, which no other spec states. OpenFIGI and EODHD serve
// the key under that ISIN. Massive takes no ISIN and does not serve it. The
// key is matched once with no finding. The spec checks the wiring to EODHD;
// how its limited answer joins OpenFIGI's is the resolve package's to test.
const fixture = path.resolve(__dirname, "..", "fixtures", "ibkr-eodhd.qfx");
const isin = "US7427181091";

test("asks EODHD beside OpenFIGI and matches the key once", async ({
  signIn,
  page,
}) => {
  const { session: userSession } = await signIn();
  await page.goto("/transactions");
  const runId = await uploadStatement(page, fixture, { state: "completed" });
  await page.getByTestId("activity-sheet-close").click();

  const { session } = await signIn("admin");
  const admin = adminRunClient(session);
  const statement = await admin.getRun({ runId });
  expect(statement.findings).toHaveLength(0);
  const resolution = statement.run!.children[0];
  await expectServedOnce(admin, resolution, ["openfigi", "eodhd"], {
    type: IdentifierType.ISIN,
    value: isin,
  });
  const massive = await fetchItems(
    admin,
    fetchRun(resolution, "massive").run!.id,
  );
  expect(massive).toHaveLength(1);
  expect(massive[0].outcome).toBe(FetchOutcome.NOT_SERVED);
  const stated = (await resolutionItems(admin, resolution.run!.id)).filter(
    (k) => k.statedKey?.identifiers.some((i) => i.value === isin),
  );
  expect(stated).toHaveLength(1);
  expect(stated[0].outcome).toBe(ResolutionOutcome.MATCHED);

  const holdings = await holdingClient(userSession).listHoldings({});
  expect(
    holdings.instruments.filter((h) =>
      h.identifiers.some((i) => i.value === isin),
    ),
  ).toHaveLength(1);
  const matched = await adminInstrumentClient(session).listInstruments({
    query: isin,
  });
  expect(matched.instruments).toHaveLength(1);
  expect(matched.instruments[0].listings.map((l) => l.currency)).toContain(
    "USD",
  );
});
