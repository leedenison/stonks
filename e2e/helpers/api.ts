import type { DescService } from "@bufbuild/protobuf";
import { type Client, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AdminService } from "../gen/admin/v1/admin_pb";
import { AuthService } from "../gen/auth/v1/auth_pb";
import { HoldingService } from "../gen/holding/v1/holding_pb";
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

export function authClient(sessionID?: string) {
  return clientFor(AuthService, sessionID);
}

export function statementClient(sessionID: string) {
  return clientFor(StatementService, sessionID);
}

export function holdingClient(sessionID: string) {
  return clientFor(HoldingService, sessionID);
}
