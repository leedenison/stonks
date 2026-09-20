import { create } from "@bufbuild/protobuf";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { GetSessionResponseSchema } from "@/gen/auth/v1/auth_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useAuthedQuery } from "./use-authed-query";

const live = liveSession();

// A session answer the test releases when it chooses, so the restoring
// window can be observed.
function gate() {
  let release: () => void = () => {};
  const opened = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { release, opened };
}

describe("useAuthedQuery", () => {
  it("waits while the session is being restored", async () => {
    const g = gate();
    const transport = transportWith({
      getSession: () => g.opened.then(() => live),
    });
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
    const transport = transportWith(create(GetSessionResponseSchema, {}));
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
    const transport = transportWith(live);
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
