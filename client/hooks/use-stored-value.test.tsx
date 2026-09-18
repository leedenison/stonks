import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useStoredValue, writeStored } from "./use-stored-value";

const parse = (raw: string | null) => raw ?? "fallback";

describe("useStoredValue", () => {
  beforeEach(() => localStorage.clear());

  it("falls back when nothing is stored and keeps what is written", () => {
    const { result } = renderHook(() => useStoredValue("k", parse));
    expect(result.current[0]).toBe("fallback");
    act(() => result.current[1]("v"));
    expect(result.current[0]).toBe("v");
    expect(localStorage.getItem("k")).toBe("v");
    act(() => result.current[1](null));
    expect(result.current[0]).toBe("fallback");
    expect(localStorage.getItem("k")).toBeNull();
  });

  it("reads what another hook writes", () => {
    const a = renderHook(() => useStoredValue("k", parse));
    const b = renderHook(() => useStoredValue("k", parse));
    act(() => a.result.current[1]("shared"));
    expect(b.result.current[0]).toBe("shared");
  });

  it("follows a storage event from another tab", () => {
    const { result } = renderHook(() => useStoredValue("k", parse));
    localStorage.setItem("k", "elsewhere");
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: "k" }));
    });
    expect(result.current[0]).toBe("elsewhere");
  });

  it("notifies without a hook", () => {
    const { result } = renderHook(() => useStoredValue("k", parse));
    act(() => writeStored("k", "direct"));
    expect(result.current[0]).toBe("direct");
  });
});
