import { create, isMessage, type MessageInitShape } from "@bufbuild/protobuf";
import { type Timestamp, timestampFromDate } from "@bufbuild/protobuf/wkt";
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
import { type UserRun, UserRunSchema } from "@/gen/admin/v1/admin_pb";
import {
  AuthService,
  type GetSessionResponse,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";

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

// authWrapper gives render and renderHook the providers a signed-in page
// needs, served over transport.
export function authWrapper(transport: Transport, client?: QueryClient) {
  const queryClient = client ?? newTestQueryClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ClientsProvider transport={transport}>
          <AuthProvider>
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

// instant reads an ISO 8601 time as a timestamp.
export function instant(iso: string): Timestamp {
  return timestampFromDate(new Date(iso));
}

// userRun builds a completed, matched run of user u1 as the admin listing
// returns it. run overrides the run's fields and rest the listing's.
export function userRun(
  run: MessageInitShape<typeof RunSchema>,
  rest: Partial<
    Pick<
      UserRun,
      | "userId"
      | "userEmail"
      | "openFindings"
      | "matched"
      | "children"
      | "datasource"
      | "endpoint"
    >
  > = {},
): UserRun {
  return create(UserRunSchema, {
    userId: "u1",
    userEmail: "one@example.com",
    matched: true,
    ...rest,
    run: create(RunSchema, { state: RunState.COMPLETED, ...run }),
  });
}
