import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyState } from "./empty-state";

describe("EmptyState", () => {
  it("states the message and offers the action", () => {
    render(
      <EmptyState
        message="Nothing yet."
        action={<button type="button">Add</button>}
      />,
    );
    expect(screen.getByTestId("empty-state").textContent).toContain(
      "Nothing yet.",
    );
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
  });
});
