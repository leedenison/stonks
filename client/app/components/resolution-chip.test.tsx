import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ResolutionOutcome } from "@/gen/type/v1/type_pb";
import { ResolutionChip } from "./resolution-chip";

describe("ResolutionChip", () => {
  it("marks each outcome", () => {
    const cases: [ResolutionOutcome, boolean, string, string][] = [
      [ResolutionOutcome.MATCHED, false, "matched", "Matched"],
      [ResolutionOutcome.REJECTED, false, "rejected", "Rejected"],
      [ResolutionOutcome.UNRECOGNISED, false, "unrecognised", "Unrecognised"],
      [ResolutionOutcome.UNAVAILABLE, false, "unavailable", "Unavailable"],
      [ResolutionOutcome.UNSPECIFIED, true, "resolving", "Resolving"],
      [ResolutionOutcome.UNSPECIFIED, false, "unresolved", "Not resolved"],
    ];
    for (const [outcome, live, state, label] of cases) {
      const { unmount } = render(
        <ResolutionChip outcome={outcome} live={live} />,
      );
      const chip = screen.getByTestId("resolution-chip");
      expect(chip.getAttribute("data-state")).toBe(state);
      expect(chip.textContent).toBe(label);
      expect(chip.getAttribute("title")).toBeTruthy();
      unmount();
    }
  });

  it("tells a key nothing recognised from one whose datasource was unavailable", () => {
    render(<ResolutionChip outcome={ResolutionOutcome.UNRECOGNISED} />);
    const unrecognised = screen.getByTestId("resolution-chip");
    expect(unrecognised.getAttribute("title")).toContain("recognised");
    render(<ResolutionChip outcome={ResolutionOutcome.UNAVAILABLE} />);
    const unavailable = screen.getAllByTestId("resolution-chip")[1];
    expect(unavailable.getAttribute("title")).toContain("replay");
    expect(unavailable.className).not.toBe(unrecognised.className);
  });
});
