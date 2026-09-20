import { create } from "@bufbuild/protobuf";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  GetSessionResponseSchema,
  Role,
  SignOutResponseSchema,
} from "@/gen/auth/v1/auth_pb";
import { schemeKey } from "@/lib/scheme";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { TopBar } from "./top-bar";

vi.mock("next/navigation", () => ({ usePathname: () => "/transactions" }));

async function openMenu() {
  await waitFor(() =>
    expect(screen.getByTestId("user-email").textContent).toBe("a@example.com"),
  );
  fireEvent.click(screen.getByTestId("user-email"));
  return screen.getByTestId("profile-menu");
}

describe("TopBar", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  it("leads home and offers a visitor the sign-in", async () => {
    const transport = transportWith(create(GetSessionResponseSchema, {}));
    renderWithAuth(<TopBar />, transport);
    expect(
      screen.getByRole("link", { name: "Stonks" }).getAttribute("href"),
    ).toBe("/");
    await waitFor(() =>
      expect(screen.getByTestId("top-bar-sign-in")).toBeTruthy(),
    );
    expect(screen.queryByTestId("user-email")).toBeNull();
  });

  it("shows a user's menu without the admin link", async () => {
    const transport = transportWith(liveSession({ role: Role.USER }));
    renderWithAuth(<TopBar />, transport);
    await openMenu();
    const sheet = screen.getByTestId("activity-sheet") as HTMLDialogElement;
    fireEvent.click(screen.getByTestId("activity-icon"));
    expect(sheet.open).toBe(true);
    fireEvent.click(screen.getByTestId("activity-icon"));
    expect(sheet.open).toBe(false);
    fireEvent.click(screen.getByTestId("activity-icon"));
    fireEvent.click(screen.getByTestId("activity-sheet-close"));
    expect(sheet.open).toBe(false);
    expect(screen.getByTestId("menu-profile").getAttribute("href")).toBe(
      "/profile",
    );
    expect(screen.getByTestId("menu-statements").getAttribute("href")).toBe(
      "/statements",
    );
    expect(screen.queryByTestId("menu-admin")).toBeNull();
    expect(screen.getByTestId("activity-icon")).toBeTruthy();
  });

  it("shows an administrator the admin link", async () => {
    const transport = transportWith(liveSession({ role: Role.ADMIN }));
    renderWithAuth(<TopBar />, transport);
    await openMenu();
    expect(screen.getByTestId("menu-admin").getAttribute("href")).toBe(
      "/admin",
    );
  });

  it("switches the scheme and keeps the choice", async () => {
    const transport = transportWith(liveSession({ role: Role.USER }));
    renderWithAuth(<TopBar />, transport);
    await openMenu();
    expect(
      screen.getByTestId("scheme-system").getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.click(screen.getByTestId("scheme-dark"));
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem(schemeKey)).toBe("dark");
    await openMenu();
    expect(screen.getByTestId("scheme-dark").getAttribute("aria-checked")).toBe(
      "true",
    );
    fireEvent.click(screen.getByTestId("scheme-system"));
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
    expect(localStorage.getItem(schemeKey)).toBeNull();
  });

  it("signs out from the menu", async () => {
    const signOut = vi.fn(() => create(SignOutResponseSchema, {}));
    const transport = transportWith({
      getSession: () => liveSession({ role: Role.USER }),
      signOut,
    });
    renderWithAuth(<TopBar />, transport);
    await openMenu();
    fireEvent.click(screen.getByTestId("sign-out"));
    await waitFor(() => expect(signOut).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByTestId("top-bar-sign-in")).toBeTruthy(),
    );
  });
});
