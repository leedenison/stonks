import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Dialog } from "./dialog";

describe("Dialog", () => {
  it("opens and closes with its open prop", () => {
    const { rerender } = render(
      <Dialog open={false} onClose={() => {}} title="T" testId="d">
        body
      </Dialog>,
    );
    const el = screen.getByTestId("d") as HTMLDialogElement;
    expect(el.open).toBe(false);
    rerender(
      <Dialog open onClose={() => {}} title="T" testId="d">
        body
      </Dialog>,
    );
    expect(el.open).toBe(true);
    expect(screen.getByRole("heading", { name: "T" })).toBeTruthy();
  });

  it("asks to close on the control, on Escape and on a click outside", () => {
    const onClose = vi.fn();
    render(
      <Dialog open onClose={onClose} title="T" testId="d">
        <p>body</p>
      </Dialog>,
    );
    const el = screen.getByTestId("d");
    fireEvent.click(screen.getByTestId("d-close"));
    fireEvent(el, new Event("cancel", { cancelable: true }));
    fireEvent.click(el);
    expect(onClose).toHaveBeenCalledTimes(3);
    fireEvent.click(screen.getByText("body"));
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});
