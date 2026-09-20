import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import AdminLayout from "./layout";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
  usePathname: () => "/admin",
}));

describe("AdminLayout", () => {
  it("denies a user", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.USER })),
    );
    await waitFor(() =>
      expect(screen.getByTestId("access-denied")).toBeTruthy(),
    );
    expect(screen.queryByText("admin child")).toBeNull();
    expect(screen.queryByTestId("admin-nav")).toBeNull();
  });

  it("shows an administrator the navigation and the page", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.ADMIN })),
    );
    await waitFor(() => expect(screen.getByText("admin child")).toBeTruthy());
    expect(screen.getByTestId("admin-nav")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Overview" })
        .getAttribute("aria-current"),
    ).toBe("page");
    expect(screen.getByText("Datasources").getAttribute("aria-disabled")).toBe(
      "true",
    );
  });
});
