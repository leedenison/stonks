import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuthService } from "@/gen/auth/v1/auth_pb";

// sessionLoss reports a session the service no longer accepts. Any call that
// fails as unauthenticated means the session is gone, except SignIn, which
// fails that way for a rejected Google token while there is no session to
// lose. The error is rethrown so the caller still sees it. Only the unary
// path is covered; a streaming call fails while its response is iterated,
// and the API has no streaming RPCs.
export function sessionLoss(onLost: () => void): Interceptor {
  return (next) => async (req) => {
    try {
      return await next(req);
    } catch (err) {
      if (
        err instanceof ConnectError &&
        err.code === Code.Unauthenticated &&
        req.method !== AuthService.method.signIn
      ) {
        onLost();
      }
      throw err;
    }
  };
}

// newTransport returns the transport every client shares. The base URL is
// relative: the edge serves the API on the page origin, so the session cookie
// travels same-site. Nothing calls the transport during prerender, because
// every fetch goes through the query client.
export function newTransport(onLost: () => void) {
  return createConnectTransport({
    baseUrl: "",
    interceptors: [sessionLoss(onLost)],
  });
}
