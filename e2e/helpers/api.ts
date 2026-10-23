import type { DescService } from "@bufbuild/protobuf";
import { type Client, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BlockService } from "../gen/admin/block/v1/block_pb";
import { DatasourceService } from "../gen/admin/datasource/v1/datasource_pb";
import { InstrumentService as AdminInstrumentService } from "../gen/admin/instrument/v1/instrument_pb";
import { ReplayService } from "../gen/admin/replay/v1/replay_pb";
import {
  FetchOutcome,
  RunService,
  type UserRun,
} from "../gen/admin/run/v1/run_pb";
import type { IdentifierType } from "../gen/type/v1/type_pb";
import { AuthService } from "../gen/auth/v1/auth_pb";
import { HoldingService } from "../gen/holding/v1/holding_pb";
import { RunService as UserRunService } from "../gen/run/v1/run_pb";
import { StatementService } from "../gen/statement/v1/statement_pb";
import { sessionCookie } from "./auth";
import { baseURL } from "./config";
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

export function adminInstrumentClient(sessionID: string) {
  return clientFor(AdminInstrumentService, sessionID);
}

export function blockClient(sessionID: string) {
  return clientFor(BlockService, sessionID);
}

export function datasourceClient(sessionID: string) {
  return clientFor(DatasourceService, sessionID);
}

export function replayClient(sessionID: string) {
  return clientFor(ReplayService, sessionID);
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

export async function statementItems(
  admin: Client<typeof RunService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "statement" ? [i.item.value] : [],
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

// fetchRun returns the fetch run below resolution that asked the named
// datasource.
export function fetchRun(resolution: UserRun, datasource: string): UserRun {
  const run = resolution.children.find((c) => c.datasource === datasource);
  if (!run) {
    throw new Error(`no fetch run asked ${datasource}`);
  }
  return run;
}

export function authClient(sessionID?: string) {
  return clientFor(AuthService, sessionID);
}

export function statementClient(sessionID: string) {
  return clientFor(StatementService, sessionID);
}

export function holdingClient(sessionID: string) {
  return clientFor(HoldingService, sessionID);
}

export function runClient(sessionID: string) {
  return clientFor(UserRunService, sessionID);
}
