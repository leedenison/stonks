import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { InfoTip } from "./info-tip";

describe("InfoTip", () => {
  it("shows the text on a mouseover and names the icon by it", () => {
    render(<InfoTip text="Why it matters." testId="tip" />);
    const tip = screen.getByRole("img", { name: "Why it matters." });
    expect(tip.getAttribute("title")).toBe("Why it matters.");
    expect(tip.getAttribute("tabindex")).toBe("0");
    expect(tip.textContent).toBe("");
  });
});
