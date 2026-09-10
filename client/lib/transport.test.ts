import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  createRouterTransport,
} from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  SignOutResponseSchema,
} from "@/gen/auth/v1/auth_pb";
import { sessionLoss } from "./transport";

function clientWith(onLost: () => void) {
  const transport = createRouterTransport(
    ({ service }) => {
      service(AuthService, {
        getSession: () => {
          throw new ConnectError("session required", Code.Unauthenticated);
        },
        signIn: () => {
          throw new ConnectError("invalid token", Code.Unauthenticated);
        },
        signOut: () => {
          throw new ConnectError("boom", Code.Internal);
        },
      });
    },
    { transport: { interceptors: [sessionLoss(onLost)] } },
  );
  return createClient(AuthService, transport);
}

describe("sessionLoss", () => {
  it("reports an unauthenticated failure and rethrows it", async () => {
    const onLost = vi.fn();
    const client = clientWith(onLost);
    await expect(client.getSession({})).rejects.toMatchObject({
      code: Code.Unauthenticated,
    });
    expect(onLost).toHaveBeenCalledTimes(1);
  });

  it("ignores a rejected sign-in", async () => {
    const onLost = vi.fn();
    const client = clientWith(onLost);
    await expect(client.signIn({ googleIdToken: "t" })).rejects.toMatchObject({
      code: Code.Unauthenticated,
    });
    expect(onLost).not.toHaveBeenCalled();
  });

  it("ignores other codes", async () => {
    const onLost = vi.fn();
    const client = clientWith(onLost);
    await expect(client.signOut({})).rejects.toMatchObject({
      code: Code.Internal,
    });
    expect(onLost).not.toHaveBeenCalled();
  });

  it("passes a success through", async () => {
    const onLost = vi.fn();
    const transport = createRouterTransport(
      ({ service }) => {
        service(AuthService, {
          signOut: () => create(SignOutResponseSchema, {}),
          getSession: () => create(GetSessionResponseSchema, {}),
        });
      },
      { transport: { interceptors: [sessionLoss(onLost)] } },
    );
    const res = await createClient(AuthService, transport).getSession({});
    expect(res.user).toBeUndefined();
    expect(onLost).not.toHaveBeenCalled();
  });
});
