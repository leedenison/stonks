import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Notice } from "./notice";

describe("Notice", () => {
  it("announces an error", () => {
    render(
      <Notice tone="error" testId="n">
        Broken
      </Notice>,
    );
    expect(screen.getByRole("alert").textContent).toBe("Broken");
    expect(screen.getByTestId("n")).toBe(screen.getByRole("alert"));
  });

  it("is a status without a retry by default", () => {
    render(<Notice>Heads up</Notice>);
    expect(screen.getByRole("status").textContent).toBe("Heads up");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("offers a retry when given one", () => {
    const onRetry = vi.fn();
    render(<Notice onRetry={onRetry}>Failed</Notice>);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
