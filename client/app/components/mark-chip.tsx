import { TxnMark } from "@/gen/stmt/v1/stmt_pb";
import { Chip, type Tone } from "./chip";

// What a transaction's chip shows. An unmarked transaction reads as resolving
// while its run is live, and as unresolved once the run has stopped.
export type MarkState =
  "resolving" | "confirmed" | "provisional" | "unresolved";

// Accent marks the one state an administrator can act on: attesting the
// ticker on that day.
const looks: Record<MarkState, { tone: Tone; label: string; title: string }> = {
  resolving: {
    tone: "muted",
    label: "Resolving",
    title: "Resolution has not reached the key yet.",
  },
  confirmed: {
    tone: "primary",
    label: "Confirmed",
    title: "A source confirms the instrument on this date, or you chose it.",
  },
  provisional: {
    tone: "muted",
    label: "Provisional",
    title:
      "Resolution assumes the instrument from the nearest fact, or from an identifier that may be reassigned.",
  },
  unresolved: {
    tone: "accent",
    label: "Unresolved",
    title:
      "The key names nothing on this date. An administrator can attest the ticker.",
  },
};

export function markState(mark: TxnMark, live: boolean): MarkState {
  switch (mark) {
    case TxnMark.CONFIRMED:
      return "confirmed";
    case TxnMark.PROVISIONAL:
      return "provisional";
    default:
      return live ? "resolving" : "unresolved";
  }
}

// MarkChip shows how firmly a transaction's key names an instrument on the
// transaction's date. live says whether the run that resolves the key is
// still running.
export function MarkChip({
  mark,
  live = false,
}: {
  mark: TxnMark;
  live?: boolean;
}) {
  const state = markState(mark, live);
  const look = looks[state];
  return (
    <Chip
      tone={look.tone}
      title={look.title}
      data-testid="mark-chip"
      data-state={state}
    >
      {look.label}
    </Chip>
  );
}
