"use client";

import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { UseQueryResult } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useState } from "react";
import type {
  ListStatementsResponse,
  StatementSummary,
} from "@/gen/statement/v1/statement_pb";
import { useRunOutcomes } from "@/hooks/use-run-outcomes";
import { useStatements } from "@/hooks/use-statements";
import { useStoredValue } from "@/hooks/use-stored-value";
import { isTerminal } from "@/lib/run";
import { useAuth } from "./auth-context";

// How many runs the sheet lists; the rest are reached through the history.
export const recentCount = 20;

type ActivityValue = {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
  // Runs that finished since the sheet was last open; zero while it is.
  badge: number;
  recent: StatementSummary[];
  statements: UseQueryResult<ListStatementsResponse>;
};

const ActivityContext = createContext<ActivityValue | null>(null);

// The moment the sheet was last open, kept per user so a badge does not
// carry from one account to another on the same browser. Nothing stored
// means the sheet has never been opened, and nothing that finished before
// counts: a first visit does not open on a badge full of history.
function seenKey(userId: string) {
  return `stonks.activity.seen.${userId}`;
}

function parseSeen(raw: string | null): number {
  return raw ? Number(raw) : Number.POSITIVE_INFINITY;
}

// ActivityProvider holds the statements every signed-in page shares: the
// sheet reads them, the badge counts them, and a run finishing refreshes
// the pages its work changed whether or not the sheet is open.
export function ActivityProvider({ children }: { children: ReactNode }) {
  const { state } = useAuth();
  const userId = state.status === "authenticated" ? state.user.id : "";
  const statements = useStatements();
  const list = statements.data?.statements ?? [];
  useRunOutcomes(list);
  const [isOpen, setOpen] = useState(false);
  const [seenAt, setSeenAt] = useStoredValue(seenKey(userId), parseSeen);

  const badge = isOpen
    ? 0
    : list.filter(
        (s) =>
          s.run?.finishedAt &&
          isTerminal(s.run.state) &&
          timestampDate(s.run.finishedAt).getTime() > seenAt,
      ).length;

  const mark = () => setSeenAt(String(Date.now()));
  const value: ActivityValue = {
    isOpen,
    open: () => {
      mark();
      setOpen(true);
    },
    close: () => {
      mark();
      setOpen(false);
    },
    toggle: () => {
      mark();
      setOpen(!isOpen);
    },
    badge,
    recent: list.slice(0, recentCount),
    statements,
  };
  return (
    <ActivityContext.Provider value={value}>
      {children}
    </ActivityContext.Provider>
  );
}

export function useActivity(): ActivityValue {
  const ctx = useContext(ActivityContext);
  if (!ctx) {
    throw new Error("useActivity called outside ActivityProvider");
  }
  return ctx;
}
