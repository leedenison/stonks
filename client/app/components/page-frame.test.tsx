import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Page } from "./page-frame";

describe("Page", () => {
  it("titles the section and holds its children", () => {
    render(
      <Page title="Things" testId="things-page">
        <p>body</p>
      </Page>,
    );
    expect(screen.getByTestId("page-title").textContent).toBe("Things");
    expect(screen.getByTestId("things-page").textContent).toContain("body");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Things",
    );
  });
});
