import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Button, LinkButton } from "./button";

describe("Button", () => {
  it("is a button of type button that calls its handler", () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Go</Button>);
    const button = screen.getByRole("button", { name: "Go" });
    expect(button.getAttribute("type")).toBe("button");
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("does nothing while disabled", () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Go
      </Button>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Go" }));
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("LinkButton", () => {
  it("is a link to its href", () => {
    render(
      <LinkButton href="/start" variant="secondary">
        Back
      </LinkButton>,
    );
    expect(
      screen.getByRole("link", { name: "Back" }).getAttribute("href"),
    ).toBe("/start");
  });
});
