"use client";

import { timestampDate } from "@bufbuild/protobuf/wkt";
import { LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";
import type { Run } from "@/gen/run/v1/run_pb";
import type { StatementSummary } from "@/gen/statement/v1/statement_pb";
import { formatElapsed } from "@/lib/format";
import { type Outcome, outcome, runOutcome } from "@/lib/run";
import { Chip, type Tone } from "./chip";

const looks: Record<Outcome, { tone: Tone; label: string }> = {
  pending: { tone: "muted", label: "Pending" },
  running: { tone: "primary", label: "Running" },
  completed: { tone: "positive", label: "Completed" },
  rejections: { tone: "accent", label: "Rejections" },
  failed: { tone: "negative", label: "Failed" },
};

// useNow is the current time, ticking once a second while active.
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [active]);
  return now;
}

// StateChip shows a run's outcome the same way everywhere. A running run
// carries a spinner and the time since it started.
export function StateChip({ summary }: { summary: StatementSummary }) {
  return <OutcomeChip o={outcome(summary)} run={summary.run} />;
}

// RunChip is StateChip for a run read without its statement.
export function RunChip({ run }: { run: Run | undefined }) {
  return <OutcomeChip o={runOutcome(run)} run={run} />;
}

function OutcomeChip({ o, run }: { o: Outcome; run: Run | undefined }) {
  const running = o === "running";
  const now = useNow(running);
  const started = run?.startedAt;
  return (
    <Chip tone={looks[o].tone} data-testid="state-chip" data-state={o}>
      {running && <LoaderCircle aria-hidden className="h-3 w-3 animate-spin" />}
      {looks[o].label}
      {running && started && (
        <span className="font-mono tabular-nums">
          {formatElapsed(now - timestampDate(started).getTime())}
        </span>
      )}
    </Chip>
  );
}
