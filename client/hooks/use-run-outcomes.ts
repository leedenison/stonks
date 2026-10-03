"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { RunState } from "@/gen/run/v1/run_pb";
import type { StatementSummary } from "@/gen/statement/v1/statement_pb";
import { qk } from "@/lib/query-keys";
import { isTerminal } from "@/lib/run";

// useRunOutcomes refreshes the data a run changed once the run finishes
// while the page is open. A run that is already terminal on first sight
// does not count as finishing.
export function useRunOutcomes(statements: StatementSummary[]) {
  const queryClient = useQueryClient();
  const seen = useRef(new Map<string, RunState>());

  useEffect(() => {
    const before = seen.current;
    const now = new Map<string, RunState>();
    const finished: string[] = [];
    for (const s of statements) {
      const run = s.run;
      if (!run) {
        continue;
      }
      now.set(run.id, run.state);
      const prior = before.get(run.id);
      if (prior !== undefined && !isTerminal(prior) && isTerminal(run.state)) {
        finished.push(run.id);
      }
    }
    seen.current = now;
    if (finished.length === 0) {
      return;
    }
    queryClient.invalidateQueries({ queryKey: qk.transactions() });
    queryClient.invalidateQueries({ queryKey: qk.holdings() });
    for (const id of finished) {
      queryClient.invalidateQueries({ queryKey: qk.statement(id) });
    }
  }, [statements, queryClient]);
}
