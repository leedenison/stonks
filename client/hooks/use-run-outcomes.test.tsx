import { create } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import { StmtSummarySchema } from "@/gen/stmt/v1/stmt_pb";
import { useRunOutcomes } from "./use-run-outcomes";

const summary = (id: string, state: RunState, live: boolean) =>
  create(StmtSummarySchema, { run: create(RunSchema, { id, state }), live });

describe("useRunOutcomes", () => {
  it("invalidates only when a statement it saw live stops being live", () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const old = summary("old", RunState.COMPLETED, false);
    const { rerender } = renderHook(({ list }) => useRunOutcomes(list), {
      wrapper,
      initialProps: { list: [old, summary("r1", RunState.PENDING, true)] },
    });
    expect(invalidate).not.toHaveBeenCalled();

    rerender({ list: [old, summary("r1", RunState.RUNNING, true)] });
    expect(invalidate).not.toHaveBeenCalled();

    // The ingest run is terminal while its follow-ups still run.
    rerender({ list: [old, summary("r1", RunState.COMPLETED, true)] });
    expect(invalidate).not.toHaveBeenCalled();

    rerender({ list: [old, summary("r1", RunState.COMPLETED, false)] });
    const keys = invalidate.mock.calls.map((c) =>
      JSON.stringify(c[0]?.queryKey),
    );
    expect(keys).toEqual([
      JSON.stringify(["holdings"]),
      JSON.stringify(["txns"]),
      JSON.stringify(["stmts", "r1"]),
    ]);

    rerender({ list: [old, summary("r1", RunState.COMPLETED, false)] });
    expect(invalidate).toHaveBeenCalledTimes(3);
  });
});
