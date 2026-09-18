import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Page } from "./page-frame";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ back: vi.fn(), push: vi.fn() }),
}));

describe("Page", () => {
  it("titles the section and holds its children", () => {
    render(
      <Page
        title="Things"
        testId="things-page"
        actions={<button type="button">Act</button>}
      >
        <p>body</p>
      </Page>,
    );
    expect(screen.getByTestId("page-title").textContent).toBe("Things");
    expect(
      screen
        .getByTestId("page-bar")
        .contains(screen.getByRole("button", { name: "Act" })),
    ).toBe(true);
    expect(screen.getByTestId("things-page").textContent).toContain("body");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Things",
    );
    expect(screen.queryByTestId("page-back")).toBeNull();
  });

  it("shows a back arrow for a page reached from another", () => {
    render(<Page title="Thing" back="/things" />);
    expect(screen.getByTestId("page-back")).toBeTruthy();
  });
});
