import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createRouterTransport,
  type ServiceImpl,
  type Transport,
} from "@connectrpc/connect";
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  SignInResponseSchema,
  SignOutResponseSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { qk } from "@/lib/query-keys";
import { authWrapper, newTestQueryClient } from "@/lib/test-utils";
import { sessionLoss } from "@/lib/transport";
import { expireSession, useAuth } from "./auth-context";

const user = create(UserSchema, {
  id: "u1",
  email: "someone@example.com",
  name: "Someone",
  role: Role.USER,
});
const session = create(SessionSchema, {
  expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
});

function transportWith(
  impl: Partial<ServiceImpl<typeof AuthService>>,
  onLost: () => void = () => {},
): Transport {
  return createRouterTransport(
    ({ service }) => {
      service(AuthService, impl);
    },
    { transport: { interceptors: [sessionLoss(onLost)] } },
  );
}

describe("AuthProvider", () => {
  it("restores a live session", async () => {
    const transport = transportWith({
      getSession: () => create(GetSessionResponseSchema, { user, session }),
    });
    const { result } = renderHook(useAuth, { wrapper: authWrapper(transport) });
    expect(result.current.state.status).toBe("restoring");
    await waitFor(() =>
      expect(result.current.state).toMatchObject({
        status: "authenticated",
        user: { email: "someone@example.com" },
      }),
    );
  });

  it("is unauthenticated when there is no session", async () => {
    const transport = transportWith({
      getSession: () => create(GetSessionResponseSchema, {}),
    });
    const { result } = renderHook(useAuth, { wrapper: authWrapper(transport) });
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
  });

  it("is unauthenticated when the restore fails", async () => {
    const transport = transportWith({
      getSession: () => {
        throw new ConnectError("down", Code.Unavailable);
      },
    });
    const { result } = renderHook(useAuth, { wrapper: authWrapper(transport) });
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
  });

  it("signs in", async () => {
    const signIn = vi.fn(() => create(SignInResponseSchema, { user, session }));
    const transport = transportWith({
      getSession: () => create(GetSessionResponseSchema, {}),
      signIn,
    });
    const { result } = renderHook(useAuth, { wrapper: authWrapper(transport) });
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
    act(() => result.current.signIn.mutate("id-token"));
    await waitFor(() =>
      expect(result.current.state.status).toBe("authenticated"),
    );
    expect(signIn).toHaveBeenCalledTimes(1);
  });

  it("surfaces a refused sign-in", async () => {
    const transport = transportWith({
      getSession: () => create(GetSessionResponseSchema, {}),
      signIn: () => {
        throw new ConnectError("email not permitted", Code.PermissionDenied);
      },
    });
    const { result } = renderHook(useAuth, { wrapper: authWrapper(transport) });
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
    act(() => result.current.signIn.mutate("id-token"));
    await waitFor(() =>
      expect(result.current.signIn.error).toMatchObject({
        code: Code.PermissionDenied,
      }),
    );
    expect(result.current.state.status).toBe("unauthenticated");
  });

  it("signs out and drops every other query", async () => {
    const transport = transportWith({
      getSession: () => create(GetSessionResponseSchema, { user, session }),
      signOut: () => create(SignOutResponseSchema, {}),
    });
    const client = newTestQueryClient();
    client.setQueryData(["holdings"], "cached");
    const { result } = renderHook(useAuth, {
      wrapper: authWrapper(transport, client),
    });
    await waitFor(() =>
      expect(result.current.state.status).toBe("authenticated"),
    );
    act(() => result.current.signOut.mutate());
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
    expect(client.getQueryData(["holdings"])).toBeUndefined();
    expect(client.getQueryData(qk.session())).toBeDefined();
  });

  it("expires when a call is refused as unauthenticated", async () => {
    const client = newTestQueryClient();
    const transport = transportWith(
      {
        getSession: () => create(GetSessionResponseSchema, { user, session }),
        signOut: () => {
          throw new ConnectError("session required", Code.Unauthenticated);
        },
      },
      () => expireSession(client),
    );
    const { result } = renderHook(useAuth, {
      wrapper: authWrapper(transport, client),
    });
    await waitFor(() =>
      expect(result.current.state.status).toBe("authenticated"),
    );
    act(() => result.current.signOut.mutate());
    await waitFor(() =>
      expect(result.current.state.status).toBe("unauthenticated"),
    );
  });
});

describe("useAuth", () => {
  it("throws outside the provider", () => {
    expect(() => renderHook(useAuth)).toThrow(/outside AuthProvider/);
  });
});
