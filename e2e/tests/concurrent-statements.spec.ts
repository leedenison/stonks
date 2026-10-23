import { RunState } from "../gen/run/v1/run_pb";
import {
  AssetClass,
  Broker,
  IdentifierType,
  ResolutionOutcome,
} from "../gen/type/v1/type_pb";
import { holdingClient, runClient, statementClient } from "../helpers/api";
import { expect, test } from "../helpers/test";

// Two statements of one user at different brokers, sent at once, each with
// one dollar cash row. Both state the same key, which resolves from the
// currency table without a fetch, so the two ingestions meet on the key's
// row and the upsert decides which of them creates it. The outcome is the
// same whichever request wins.
const cash = {
  assetClass: AssetClass.CASH,
  currency: "USD",
  identifiers: [{ type: IdentifierType.CURRENCY, value: "USD" }],
};

function statement(broker: Broker, quantity: string) {
  return {
    broker,
    orderFrom: "2026-03-01",
    orderBefore: "2026-04-01",
    rows: [
      {
        key: cash,
        orderDate: "2026-03-05",
        settlementDate: "2026-03-05",
        asAt: "2026-03-05",
        quantity,
      },
    ],
  };
}

test("two statements sent at once share one cash key", async ({ seed }) => {
  const { session } = await seed();
  const statements = statementClient(session);
  const runs = runClient(session);
  const started = await Promise.all([
    statements.createStatement({ statement: statement(Broker.IBKR, "100") }),
    statements.createStatement({ statement: statement(Broker.SCHWAB, "250") }),
  ]);
  const ids = started.map((r) => r.run!.id);
  for (const runId of ids) {
    await expect
      .poll(async () => (await runs.getRun({ runId })).run?.state)
      .toBe(RunState.COMPLETED);
  }

  const keys = await Promise.all(
    ids.map(async (runId) => (await statements.getStatement({ runId })).keys),
  );
  for (const k of keys) {
    expect(k).toHaveLength(1);
    expect(k[0].outcome).toBe(ResolutionOutcome.MATCHED);
  }
  expect(keys[0][0].statedKeyId).toBe(keys[1][0].statedKeyId);

  const holdings = await holdingClient(session).listHoldings({});
  expect(holdings.groups).toHaveLength(0);
  expect(holdings.instruments).toHaveLength(1);
  expect(holdings.instruments[0].quantity).toBe("350");
  expect(holdings.instruments[0].keys).toHaveLength(1);
});
