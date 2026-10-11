import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Sidebar, sidebarKey } from "./sidebar";

const nav = vi.hoisted(() => ({ pathname: "/transactions" }));
vi.mock("next/navigation", () => ({ usePathname: () => nav.pathname }));

describe("Sidebar", () => {
  beforeEach(() => localStorage.clear());

  it("marks the current page and links the others", () => {
    render(<Sidebar />);
    expect(
      screen.getByTestId("nav-transactions").getAttribute("aria-current"),
    ).toBe("page");
    expect(
      screen.getByTestId("nav-holdings").hasAttribute("aria-current"),
    ).toBe(false);
    expect(screen.getByTestId("nav-holdings").getAttribute("href")).toBe(
      "/holdings",
    );
  });

  it("marks a page from a path beneath it", () => {
    nav.pathname = "/holdings/x";
    render(<Sidebar />);
    expect(
      screen.getByTestId("nav-holdings").getAttribute("aria-current"),
    ).toBe("page");
    nav.pathname = "/transactions";
  });

  it("collapses by hand and keeps the choice", () => {
    render(<Sidebar />);
    const aside = screen.getByTestId("sidebar");
    expect(aside.hasAttribute("data-collapsed")).toBe(false);
    fireEvent.click(screen.getByTestId("sidebar-collapse"));
    expect(aside.hasAttribute("data-collapsed")).toBe(true);
    expect(localStorage.getItem(sidebarKey)).toBe("collapsed");
    fireEvent.click(screen.getByTestId("sidebar-collapse"));
    expect(aside.hasAttribute("data-collapsed")).toBe(false);
    expect(localStorage.getItem(sidebarKey)).toBeNull();
  });
});
