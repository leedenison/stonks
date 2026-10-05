import type { DescService } from "@bufbuild/protobuf";
import { type Client, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AdminService } from "../gen/admin/v1/admin_pb";
import { AuthService } from "../gen/auth/v1/auth_pb";
import { HoldingService } from "../gen/holding/v1/holding_pb";
import { InstrumentService } from "../gen/instrument/v1/instrument_pb";
import { StatementService } from "../gen/statement/v1/statement_pb";
import { sessionCookie } from "./auth";
import { baseURL } from "./config";

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

export function adminClient(sessionID: string) {
  return clientFor(AdminService, sessionID);
}

// items reads the items a run wrote. Every run a spec makes writes few
// enough for one page, which the read asserts.
async function items(admin: Client<typeof AdminService>, runId: string) {
  const res = await admin.listRunItems({ runId });
  if (res.nextPageToken !== "") {
    throw new Error(`run ${runId} has more than one page of items`);
  }
  return res.items;
}

export async function statementItems(
  admin: Client<typeof AdminService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "statement" ? [i.item.value] : [],
  );
}

export async function resolutionItems(
  admin: Client<typeof AdminService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "resolution" ? [i.item.value] : [],
  );
}

export async function fetchItems(
  admin: Client<typeof AdminService>,
  runId: string,
) {
  return (await items(admin, runId)).flatMap((i) =>
    i.item.case === "fetch" ? [i.item.value] : [],
  );
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

export function instrumentClient(sessionID: string) {
  return clientFor(InstrumentService, sessionID);
}
