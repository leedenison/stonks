import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { useDebounced } from "./use-debounced";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

test("settles on the last value once it holds still for the delay", () => {
  const { result, rerender } = renderHook(({ v }) => useDebounced(v, 300), {
    initialProps: { v: "a" },
  });
  rerender({ v: "ab" });
  act(() => vi.advanceTimersByTime(200));
  rerender({ v: "abc" });
  act(() => vi.advanceTimersByTime(200));
  expect(result.current).toBe("a");
  act(() => vi.advanceTimersByTime(100));
  expect(result.current).toBe("abc");
});
