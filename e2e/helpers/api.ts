import type { DescService } from "@bufbuild/protobuf";
import { type Client, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BlockService, FetchKind } from "../gen/admin/block/v1/block_pb";
import { CorpActService } from "../gen/admin/corpact/v1/corpact_pb";
import { CoverageService } from "../gen/admin/coverage/v1/coverage_pb";
import { DatasourceService } from "../gen/admin/datasource/v1/datasource_pb";
import { InstrService as AdminInstrService } from "../gen/admin/instr/v1/instr_pb";
import { ReplayRunService } from "../gen/admin/replay_run/v1/replay_run_pb";
import {
  FetchOutcome,
  type Finding,
  FindingKind,
  RunService,
  type UserRun,
} from "../gen/admin/run/v1/run_pb";
import type { Identifier, IdentifierType } from "../gen/type/v1/type_pb";
import type { Instr } from "../gen/admin/instr/v1/instr_pb";
import { AuthService } from "../gen/auth/v1/auth_pb";
import { HoldingService } from "../gen/holding/v1/holding_pb";
import { RunState, RunService as UserRunService } from "../gen/run/v1/run_pb";
import { StmtService } from "../gen/stmt/v1/stmt_pb";
import { sessionCookie } from "./auth";
import { baseURL, recording } from "./config";
import { expect } from "./test";

// clientFor returns the generated client of service over the edge, carrying
// the session as the browser would. Node's fetch keeps no cookies, so the
// header is set on every call.
export function clientFor<T extends DescService>(
  service: T,
  sessionID?: string,
): Client<T> {
  const transport = createConnectTransport({
    baseUrl: baseURL,
    interceptors: [
      (next) => (req) => {
        if (sessionID) {
          req.header.set("Cookie", sessionCookie(sessionID));
        }
        return next(req);
      },
    ],
  });
  return createClient(service, transport);
}

export function adminRunClient(sessionID: string) {
  return clientFor(RunService, sessionID);
}

export function adminInstrClient(sessionID: string) {
  return clientFor(AdminInstrService, sessionID);
}

export function blockClient(sessionID: string) {
  return clientFor(BlockService, sessionID);
}

export function corpActClient(sessionID: string) {
  return clientFor(CorpActService, sessionID);
}

export function coverageClient(sessionID: string) {
  return clientFor(CoverageService, sessionID);
}

export function datasourceClient(sessionID: string) {
  return clientFor(DatasourceService, sessionID);
}

export function replayRunClient(sessionID: string) {
  return clientFor(ReplayRunService, sessionID);
}

// items reads the items a run wrote. Every run a spec makes writes few
// enough for one page, which the read asserts.
async function items(admin: Client<typeof RunService>, runId: string) {
  const res = await admin.listRunItems({ runId });
  if (res.nextPageToken !== "") {
    throw new Error(`run ${runId} has more than one page of items`);
  }
  return res.items;
}

export async function stmtItems(
  admin: Client<typeof RunService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "stmt" ? [i.item.value] : [],
  );
}

export async function resolutionItems(
  admin: Client<typeof RunService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "resolution" ? [i.item.value] : [],
  );
}

export async function assertionItems(
  admin: Client<typeof RunService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "assertion" ? [i.item.value] : [],
  );
}

export async function fetchItems(
  admin: Client<typeof RunService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "fetch" ? [i.item.value] : [],
  );
}

// expectServedOnce asserts that each named datasource, run below resolution,
// was asked exactly once for the identifier sent and served it.
export async function expectServedOnce(
  admin: Client<typeof RunService>,
  resolution: UserRun,
  names: string[],
  sent: { type: IdentifierType; value: string },
): Promise<void> {
  for (const name of names) {
    const fetched = await fetchItems(admin, fetchRun(resolution, name).run!.id);
    expect(fetched, name).toHaveLength(1);
    expect(fetched[0].outcome, name).toBe(FetchOutcome.SERVED);
    expect(fetched[0].sent?.type, name).toBe(sent.type);
    expect(fetched[0].sent?.value, name).toBe(sent.value);
  }
}

// fetchRun returns the datasource fetch run below resolution that asked the
// named datasource for data of kind.
export function fetchRun(
  resolution: UserRun,
  datasource: string,
  kind = FetchKind.IDENTITY,
): UserRun {
  const run = resolution.children.find(
    (c) => c.fetch?.datasource === datasource && c.fetch.kind === kind,
  );
  if (!run) {
    throw new Error(`no datasource fetch run asked ${datasource}`);
  }
  return run;
}

export function authClient(sessionID?: string) {
  return clientFor(AuthService, sessionID);
}

export function stmtClient(sessionID: string) {
  return clientFor(StmtService, sessionID);
}

export function holdingClient(sessionID: string) {
  return clientFor(HoldingService, sessionID);
}

export function runClient(sessionID: string) {
  return clientFor(UserRunService, sessionID);
}

// instrListedAs returns the one instrument that has a listing named by
// ticker at its venue, and fails the test unless exactly one has. The
// instruments search matches any part of a name, so the hits are narrowed to
// those whose listings carry the ticker exactly.
export async function instrListedAs(
  sessionID: string,
  ticker: Pick<Identifier, "domain" | "value">,
): Promise<Instr> {
  const listed = await adminInstrClient(sessionID).listInstrs({
    query: ticker.value,
  });
  const named = listed.instrs.filter((i) =>
    i.listings.some((l) =>
      l.tickers.some(
        (t) => t.mic === ticker.domain && t.ticker?.value === ticker.value,
      ),
    ),
  );
  expect(named, `${ticker.domain}:${ticker.value}`).toHaveLength(1);
  return named[0];
}

// settled waits until the statement that the ingest statement run runId
// ingested stops being live, so nothing it started is still running.
export async function settled(
  stmts: Client<typeof StmtService>,
  runId: string,
): Promise<void> {
  await expect
    .poll(async () => (await stmts.getStmt({ runId })).stmt?.live, {
      intervals: [500],
      timeout: recording ? 0 : 30_000,
    })
    .toBe(false);
}

// stopped waits until the run runId and every run beneath it have stopped.
export async function stopped(
  admin: Client<typeof RunService>,
  runId: string,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const got = (await admin.getRun({ runId })).run;
        const runs: UserRun[] = [];
        const walk = (r: UserRun) => {
          runs.push(r);
          r.children.forEach(walk);
        };
        if (got) {
          walk(got);
        }
        return (
          runs.length > 0 && runs.every((r) => stoppedStates.has(r.run!.state))
        );
      },
      { intervals: [500], timeout: recording ? 0 : 30_000 },
    )
    .toBe(true);
}

const stoppedStates = new Set([
  RunState.COMPLETED,
  RunState.FAILED,
  RunState.INTERRUPTED,
]);

// resolutionFindings leaves out the findings of corporate actions fetches,
// which follow a statement whatever its resolution found.
export function resolutionFindings(findings: Finding[]): Finding[] {
  return findings.filter(
    (f) =>
      f.kind !== FindingKind.UNMATCHED_SPLIT &&
      f.kind !== FindingKind.UNHANDLED_CORP_ACT,
  );
}
