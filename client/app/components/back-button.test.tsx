import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BackButton } from "./back-button";

describe("BackButton", () => {
  it("leads to the page given", () => {
    render(<BackButton to="/statements" />);
    expect(screen.getByTestId("page-back").getAttribute("href")).toBe(
      "/statements",
    );
  });
});
