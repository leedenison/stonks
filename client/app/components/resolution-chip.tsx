import { ResolutionOutcome } from "@/gen/type/v1/type_pb";
import { Chip, type Tone } from "./chip";

// What a key's chip shows. A key with no recorded outcome shows as resolving
// while its run is live, and as unresolved once the run has stopped, since a
// failed or interrupted resolution leaves keys behind.
export type ResolutionState =
  | "resolving"
  | "unresolved"
  | "matched"
  | "rejected"
  | "unrecognised"
  | "unavailable";

// Accent marks the states an administrator acts on, as it marks rejections
// elsewhere; the positive and negative tones are reserved for run outcomes.
const looks: Record<
  ResolutionState,
  { tone: Tone; label: string; title: string }
> = {
  resolving: {
    tone: "muted",
    label: "Resolving",
    title: "Resolution has not reached the key yet.",
  },
  unresolved: {
    tone: "muted",
    label: "Not resolved",
    title: "No resolution recorded an outcome for the key.",
  },
  matched: {
    tone: "primary",
    label: "Matched",
    title: "The key names an instrument.",
  },
  rejected: {
    tone: "accent",
    label: "Rejected",
    title: "The key contradicts reference data, so its rows were refused.",
  },
  unrecognised: {
    tone: "muted",
    label: "Unrecognised",
    title: "No datasource recognised what the statement states.",
  },
  unavailable: {
    tone: "accent",
    label: "Unavailable",
    title:
      "A datasource failed or was blocked. An administrator can replay the key.",
  },
};

export function resolutionState(
  outcome: ResolutionOutcome,
  live: boolean,
): ResolutionState {
  switch (outcome) {
    case ResolutionOutcome.MATCHED:
      return "matched";
    case ResolutionOutcome.REJECTED:
      return "rejected";
    case ResolutionOutcome.UNRECOGNISED:
      return "unrecognised";
    case ResolutionOutcome.UNAVAILABLE:
      return "unavailable";
    default:
      return live ? "resolving" : "unresolved";
  }
}

// ResolutionChip shows what resolution made of a stated key the same way
// everywhere. live says whether the run that resolves the key is still
// running.
export function ResolutionChip({
  outcome,
  live = false,
}: {
  outcome: ResolutionOutcome;
  live?: boolean;
}) {
  const state = resolutionState(outcome, live);
  const look = looks[state];
  return (
    <Chip
      tone={look.tone}
      title={look.title}
      data-testid="resolution-chip"
      data-state={state}
    >
      {look.label}
    </Chip>
  );
}
