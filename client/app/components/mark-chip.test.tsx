import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TransactionMark } from "@/gen/statement/v1/statement_pb";
import { MarkChip } from "./mark-chip";

describe("MarkChip", () => {
  it("marks each state", () => {
    const cases: [TransactionMark, boolean, string, string][] = [
      [TransactionMark.CONFIRMED, false, "confirmed", "Confirmed"],
      [TransactionMark.PROVISIONAL, false, "provisional", "Provisional"],
      [TransactionMark.UNRESOLVED, false, "unresolved", "Unresolved"],
      [TransactionMark.UNRESOLVED, true, "resolving", "Resolving"],
      [TransactionMark.UNSPECIFIED, true, "resolving", "Resolving"],
    ];
    for (const [mark, live, state, label] of cases) {
      const { unmount } = render(<MarkChip mark={mark} live={live} />);
      const chip = screen.getByTestId("mark-chip");
      expect(chip.getAttribute("data-state")).toBe(state);
      expect(chip.textContent).toBe(label);
      expect(chip.getAttribute("title")).toBeTruthy();
      unmount();
    }
  });
});
