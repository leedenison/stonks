import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ActivityIcon } from "./activity-icon";

describe("ActivityIcon", () => {
  it("hides the badge at zero", () => {
    render(<ActivityIcon count={0} />);
    expect(screen.getByTestId("activity-icon")).toBeTruthy();
    expect(screen.queryByTestId("activity-badge")).toBeNull();
  });

  it("shows the count and calls its handler", () => {
    const onClick = vi.fn();
    render(<ActivityIcon count={3} onClick={onClick} />);
    expect(screen.getByTestId("activity-badge").textContent).toBe("3");
    fireEvent.click(screen.getByRole("button", { name: "Activity" }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
