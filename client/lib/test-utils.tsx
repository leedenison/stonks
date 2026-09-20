import type { Transport } from "@connectrpc/connect";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactNode } from "react";
import { ActivityProvider } from "@/contexts/activity-context";
import { AuthProvider } from "@/contexts/auth-context";
import { ClientsProvider } from "@/contexts/clients-context";

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
