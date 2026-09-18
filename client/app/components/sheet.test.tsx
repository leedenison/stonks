import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Sheet } from "./sheet";

describe("Sheet", () => {
  it("opens with its prop and asks to close on the control, Escape and the overlay", () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <Sheet open={false} onClose={onClose} title="Side" testId="s">
        <p>body</p>
      </Sheet>,
    );
    const el = screen.getByTestId("s") as HTMLDialogElement;
    expect(el.open).toBe(false);
    expect(screen.queryByTestId("s-overlay")).toBeNull();
    rerender(
      <Sheet open onClose={onClose} title="Side" testId="s">
        <p>body</p>
      </Sheet>,
    );
    expect(el.open).toBe(true);
    expect(screen.getByRole("heading", { name: "Side" })).toBeTruthy();
    fireEvent.click(screen.getByTestId("s-close"));
    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.click(screen.getByTestId("s-overlay"));
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});
