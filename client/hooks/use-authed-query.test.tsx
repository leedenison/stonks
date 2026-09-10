import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { authWrapper } from "@/lib/test-utils";
import { useAuthedQuery } from "./use-authed-query";

const live = create(GetSessionResponseSchema, {
  user: create(UserSchema, {
    id: "u1",
    email: "a@example.com",
    role: Role.USER,
  }),
  session: create(SessionSchema, {
    expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
  }),
});

// A session answer the test releases when it chooses, so the restoring
// window can be observed.
function gate() {
  let release: () => void = () => {};
  const opened = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { release, opened };
}

function transportAnswering(answer: () => Promise<typeof live> | typeof live) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: answer });
  });
}

describe("useAuthedQuery", () => {
  it("waits while the session is being restored", async () => {
    const g = gate();
    const transport = transportAnswering(() => g.opened.then(() => live));
    const queryFn = vi.fn(() => Promise.resolve("data"));
    const { result } = renderHook(
      () => useAuthedQuery({ queryKey: ["thing"], queryFn }),
      { wrapper: authWrapper(transport) },
    );
    await Promise.resolve();
    expect(queryFn).not.toHaveBeenCalled();
    expect(result.current.fetchStatus).toBe("idle");

    g.release();
    await waitFor(() => expect(result.current.data).toBe("data"));
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("does not run without a session", async () => {
    const transport = transportAnswering(() =>
      create(GetSessionResponseSchema, {}),
    );
    const queryFn = vi.fn(() => Promise.resolve("data"));
    const { result } = renderHook(
      () => useAuthedQuery({ queryKey: ["thing"], queryFn }),
      { wrapper: authWrapper(transport) },
    );
    await waitFor(() => expect(result.current.isPending).toBe(true));
    await Promise.resolve();
    expect(queryFn).not.toHaveBeenCalled();
    expect(result.current.fetchStatus).toBe("idle");
  });

  it("respects the caller's enabled", async () => {
    const transport = transportAnswering(() => live);
    const queryFn = vi.fn(() => Promise.resolve("data"));
    const { result } = renderHook(
      () => useAuthedQuery({ queryKey: ["thing"], queryFn, enabled: false }),
      { wrapper: authWrapper(transport) },
    );
    await Promise.resolve();
    await Promise.resolve();
    expect(queryFn).not.toHaveBeenCalled();
    expect(result.current.fetchStatus).toBe("idle");
  });
});
