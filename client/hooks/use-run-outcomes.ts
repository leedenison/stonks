"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { StmtSummary } from "@/gen/stmt/v1/stmt_pb";
import { invalidateResolved } from "@/lib/query-keys";

// useRunOutcomes refreshes the data a statement changed when the statement
// stops being live while the page is open. A statement first seen not live is
// ignored.
export function useRunOutcomes(stmts: StmtSummary[]) {
  const queryClient = useQueryClient();
  const seen = useRef(new Map<string, boolean>());

  useEffect(() => {
    const before = seen.current;
    const now = new Map<string, boolean>();
    const finished: string[] = [];
    for (const s of stmts) {
      const id = s.run?.id;
      if (!id) {
        continue;
      }
      now.set(id, s.live);
      if (before.get(id) === true && !s.live) {
        finished.push(id);
      }
    }
    seen.current = now;
    if (finished.length === 0) {
      return;
    }
    invalidateResolved(queryClient, finished);
  }, [stmts, queryClient]);
}
