import { create, isMessage, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  type ConnectRouter,
  createRouterTransport,
  type ServiceImpl,
  type Transport,
} from "@connectrpc/connect";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactNode } from "react";
import { ActivityProvider } from "@/contexts/activity-context";
import { AuthProvider } from "@/contexts/auth-context";
import { ClientsProvider } from "@/contexts/clients-context";
import {
  AuthService,
  type GetSessionResponse,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";

// Test support for components under the auth provider. A component that
// calls useRouter also needs next/navigation mocked in its test file, since
// the App Router is not mounted under jsdom:
//
//   vi.mock("next/navigation", () => ({ useRouter: () => router }));

// newTestQueryClient returns a client that surfaces a failure at once and
// forgets a query as soon as it is unobserved, so nothing leaks between tests.
export function newTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

// authWrapper returns a wrapper mounting the query client, the clients, the
// auth provider and the activity provider over transport, for render and
// renderHook.
export function authWrapper(transport: Transport, client?: QueryClient) {
  const queryClient = client ?? newTestQueryClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ClientsProvider transport={transport}>
          <AuthProvider transport={transport}>
            <ActivityProvider>{children}</ActivityProvider>
          </AuthProvider>
        </ClientsProvider>
      </QueryClientProvider>
    );
  };
}

// renderWithAuth renders ui under the auth provider over transport.
export function renderWithAuth(
  ui: ReactNode,
  transport: Transport,
  client?: QueryClient,
): RenderResult {
  return render(ui, { wrapper: authWrapper(transport, client) });
}

// liveSession is GetSession's answer for a signed-in user. user overrides the
// default identity and expiresAt the session's end.
export function liveSession(
  user?: MessageInitShape<typeof UserSchema>,
  expiresAt = new Date("2026-09-16T00:00:00Z"),
): GetSessionResponse {
  return create(GetSessionResponseSchema, {
    user: create(UserSchema, {
      id: "u1",
      email: "a@example.com",
      role: Role.USER,
      ...user,
    }),
    session: create(SessionSchema, { expiresAt: timestampFromDate(expiresAt) }),
  });
}

// transportWith serves AuthService, answering GetSession with auth when it is
// a response and with auth's methods otherwise, plus whatever mount registers.
// The router serves every method of a service from one registration, so an
// auth method a test needs goes in auth rather than in mount.
export function transportWith(
  auth: GetSessionResponse | Partial<ServiceImpl<typeof AuthService>>,
  mount?: (router: ConnectRouter) => void,
  options?: Parameters<typeof createRouterTransport>[1],
): Transport {
  const impl = isMessage(auth, GetSessionResponseSchema)
    ? { getSession: () => auth }
    : auth;
  return createRouterTransport((router) => {
    router.service(AuthService, impl);
    mount?.(router);
  }, options);
}
