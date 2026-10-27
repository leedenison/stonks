import path from "node:path";
import { IdentifierType, ResolutionOutcome } from "../gen/type/v1/type_pb";
import {
  adminInstrClient,
  adminRunClient,
  expectServedOnce,
  holdingClient,
  resolutionFindings,
  resolutionItems,
} from "../helpers/api";
import { uploadStmt } from "../helpers/upload";
import { expect, test } from "../helpers/test";

// One IBKR line, derived from the client's IBKR test export, which is
// modelled on a real export with its identifiers replaced. It states the
// CUSIP of Johnson & Johnson, which no other spec states. OpenFIGI and Massive
// both serve the key under that CUSIP, and the key is matched once with no
// finding. The trade's cash leg states a second key, USD, which no datasource
// is asked about. The spec checks the wiring to Massive; how its
// limited answer joins OpenFIGI's is the resolve package's to test.
const fixture = path.resolve(__dirname, "..", "fixtures", "ibkr-massive.qfx");
const cusip = "478160104";

test("asks Massive beside OpenFIGI and matches the key once", async ({
  signIn,
  page,
}) => {
  const { session: userSession } = await signIn();
  await page.goto("/txns");
  const runId = await uploadStmt(page, fixture, { state: "completed" });
  await page.getByTestId("activity-sheet-close").click();

  const { session } = await signIn("admin");
  const admin = adminRunClient(session);
  const stmt = await admin.getRun({ runId });
  expect(resolutionFindings(stmt.findings)).toHaveLength(0);
  const resolution = stmt.run!.children[0];
  await expectServedOnce(admin, resolution, ["openfigi", "massive"], {
    type: IdentifierType.CUSIP,
    value: cusip,
  });
  const stated = (await resolutionItems(admin, resolution.run!.id)).filter(
    (k) => k.statedKey?.identifiers.some((i) => i.value === cusip),
  );
  expect(stated).toHaveLength(1);
  expect(stated[0].outcome).toBe(ResolutionOutcome.MATCHED);

  const holdings = await holdingClient(userSession).listHoldings({});
  expect(holdings.groups).toHaveLength(0);
  const held = holdings.instrs.filter((h) =>
    h.identifiers.some((i) => i.value === cusip),
  );
  expect(held).toHaveLength(1);
  expect(held[0].listings.map((l) => l.venue)).toContain("NYSE");
  const matched = await adminInstrClient(session).listInstrs({
    query: cusip,
  });
  expect(matched.instrs).toHaveLength(1);
  const usd = matched.instrs[0].listings.find((l) => l.currency === "USD");
  expect(usd?.primaryMic).toBe("XNYS");
});
