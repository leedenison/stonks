"use client";

import { create } from "@bufbuild/protobuf";
import type { Transport } from "@connectrpc/connect";
import {
  type QueryClient,
  type UseMutationResult,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useMemo } from "react";
import {
  GetSessionResponseSchema,
  type Session,
  type SignInResponse,
  type SignOutResponse,
  type User,
} from "@/gen/auth/v1/auth_pb";
import { newClients } from "@/lib/clients";
import { qk } from "@/lib/query-keys";

// The session as the client knows it. Restoring lasts from mount until
// GetSession answers; nothing that needs a session fires meanwhile.
export type AuthState =
  | { status: "restoring" }
  | { status: "unauthenticated" }
  | { status: "authenticated"; user: User; session: Session };

type AuthValue = {
  state: AuthState;
  signIn: UseMutationResult<SignInResponse, Error, string>;
  signOut: UseMutationResult<SignOutResponse, Error, void>;
};

const AuthContext = createContext<AuthValue | null>(null);

// expireSession records that the service no longer accepts the session. The
// session query is overwritten first, so the state is unauthenticated before
// any other query is removed and nothing gated on it refetches.
export function expireSession(client: QueryClient) {
  client.setQueryData(qk.session(), create(GetSessionResponseSchema, {}));
  client.removeQueries({ predicate: (q) => q.queryKey[0] !== "session" });
}

// AuthProvider holds the session and the sign-in and sign-out mutations. The
// session is restored through the session query on mount, so a stored value
// paints at once and is revalidated behind it. A failed restore is treated
// as no session: a network fault shows the sign-in rather than an error.
export function AuthProvider({
  transport,
  children,
}: {
  transport: Transport;
  children: ReactNode;
}) {
  const queryClient = useQueryClient();
  const clients = useMemo(() => newClients(transport), [transport]);

  const session = useQuery({
    queryKey: qk.session(),
    queryFn: () => clients.auth.getSession({}),
  });

  const signIn = useMutation({
    mutationFn: (googleIdToken: string) =>
      clients.auth.signIn({ googleIdToken }),
    onSuccess: (res) => {
      queryClient.setQueryData(
        qk.session(),
        create(GetSessionResponseSchema, {
          user: res.user,
          session: res.session,
        }),
      );
    },
  });

  const signOut = useMutation({
    mutationFn: () => clients.auth.signOut({}),
    onSuccess: () => expireSession(queryClient),
  });

  const state = useMemo<AuthState>(() => {
    if (session.isPending) {
      return { status: "restoring" };
    }
    const user = session.data?.user;
    const current = session.data?.session;
    if (user && current) {
      return { status: "authenticated", user, session: current };
    }
    return { status: "unauthenticated" };
  }, [session.isPending, session.data]);

  // The value is rebuilt on each render rather than memoised: a mutation
  // result is a new object every time, so a memo over one never holds.
  return (
    <AuthContext.Provider value={{ state, signIn, signOut }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth called outside AuthProvider");
  }
  return ctx;
}
