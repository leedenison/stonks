import { create } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import { StatementSummarySchema } from "@/gen/statement/v1/statement_pb";
import { useRunOutcomes } from "./use-run-outcomes";

const summary = (id: string, state: RunState) =>
  create(StatementSummarySchema, { run: create(RunSchema, { id, state }) });

describe("useRunOutcomes", () => {
  it("invalidates only when a run it saw live turns terminal", () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { rerender } = renderHook(({ list }) => useRunOutcomes(list), {
      wrapper,
      initialProps: {
        list: [
          summary("old", RunState.COMPLETED),
          summary("r1", RunState.PENDING),
        ],
      },
    });
    expect(invalidate).not.toHaveBeenCalled();

    rerender({
      list: [
        summary("old", RunState.COMPLETED),
        summary("r1", RunState.RUNNING),
      ],
    });
    expect(invalidate).not.toHaveBeenCalled();

    rerender({
      list: [
        summary("old", RunState.COMPLETED),
        summary("r1", RunState.COMPLETED),
      ],
    });
    const keys = invalidate.mock.calls.map((c) =>
      JSON.stringify(c[0]?.queryKey),
    );
    expect(keys).toEqual([
      JSON.stringify(["transactions"]),
      JSON.stringify(["holdings"]),
      JSON.stringify(["statements", "r1"]),
    ]);

    rerender({
      list: [
        summary("old", RunState.COMPLETED),
        summary("r1", RunState.COMPLETED),
      ],
    });
    expect(invalidate).toHaveBeenCalledTimes(3);
  });
});
