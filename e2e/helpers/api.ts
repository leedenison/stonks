import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuthService } from "../gen/auth/v1/auth_pb";
import { sessionCookie } from "./auth";
import { baseURL } from "./config";

// authClient returns the generated auth client over the edge, carrying the
// session as the browser would. Node's fetch keeps no cookies, so the header
// is set on every call.
export function authClient(sessionID?: string) {
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
  return createClient(AuthService, transport);
}
