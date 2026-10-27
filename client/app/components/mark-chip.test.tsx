import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TxnMark } from "@/gen/stmt/v1/stmt_pb";
import { MarkChip } from "./mark-chip";

describe("MarkChip", () => {
  it("marks each state", () => {
    const cases: [TxnMark, boolean, string, string][] = [
      [TxnMark.CONFIRMED, false, "confirmed", "Confirmed"],
      [TxnMark.PROVISIONAL, false, "provisional", "Provisional"],
      [TxnMark.UNRESOLVED, false, "unresolved", "Unresolved"],
      [TxnMark.UNRESOLVED, true, "resolving", "Resolving"],
      [TxnMark.UNSPECIFIED, true, "resolving", "Resolving"],
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
