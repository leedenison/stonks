import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Menu, MenuItem, MenuLink, MenuSeparator } from "./menu";

const nav = vi.hoisted(() => ({ pathname: "/a" }));
vi.mock("next/navigation", () => ({ usePathname: () => nav.pathname }));

function subject(onSelect = vi.fn()) {
  return (
    <Menu label="Open" testId="trigger" panelTestId="panel">
      <MenuLink href="/x" testId="link">
        Link
      </MenuLink>
      <MenuSeparator />
      <MenuItem onSelect={onSelect} testId="item" selected>
        Item
      </MenuItem>
    </Menu>
  );
}

describe("Menu", () => {
  it("opens on the trigger and closes on an item", () => {
    const onSelect = vi.fn();
    render(subject(onSelect));
    expect(screen.queryByTestId("panel")).toBeNull();
    fireEvent.click(screen.getByTestId("trigger"));
    expect(screen.getByTestId("trigger").getAttribute("aria-expanded")).toBe(
      "true",
    );
    expect(screen.getByRole("menuitemradio").getAttribute("aria-checked")).toBe(
      "true",
    );
    fireEvent.click(screen.getByTestId("item"));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("panel")).toBeNull();
  });

  it("closes on a link", () => {
    render(subject());
    fireEvent.click(screen.getByTestId("trigger"));
    expect(screen.getByTestId("link").getAttribute("href")).toBe("/x");
    fireEvent.click(screen.getByTestId("link"));
    expect(screen.queryByTestId("panel")).toBeNull();
  });

  it("closes on Escape and on a click outside", () => {
    render(subject());
    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("panel")).toBeNull();
    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.mouseDown(document.body);
    expect(screen.queryByTestId("panel")).toBeNull();
  });

  it("closes when the route changes", () => {
    const { rerender } = render(subject());
    fireEvent.click(screen.getByTestId("trigger"));
    expect(screen.getByTestId("panel")).toBeTruthy();
    nav.pathname = "/b";
    rerender(subject());
    expect(screen.queryByTestId("panel")).toBeNull();
    nav.pathname = "/a";
  });
});
