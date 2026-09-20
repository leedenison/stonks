import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import ProfilePage from "./page";

function transportFor(name: string, role: Role) {
  return transportWith(
    liveSession({ name, role }, new Date("2026-09-16T08:30:00Z")),
  );
}

describe("ProfilePage", () => {
  it("renders the account", async () => {
    renderWithAuth(<ProfilePage />, transportFor("Someone", Role.ADMIN));
    await waitFor(() =>
      expect(screen.getByTestId("profile-page")).toBeTruthy(),
    );
    expect(screen.getByTestId("profile-email").textContent).toBe(
      "a@example.com",
    );
    expect(screen.getByTestId("profile-name").textContent).toBe("Someone");
    expect(screen.getByTestId("profile-role").textContent).toBe("admin");
    expect(screen.getByTestId("profile-expires").textContent).toBe(
      "2026-09-16 08:30 UTC",
    );
  });

  it("marks a name Google did not report", async () => {
    renderWithAuth(<ProfilePage />, transportFor("", Role.USER));
    await waitFor(() =>
      expect(screen.getByTestId("profile-page")).toBeTruthy(),
    );
    expect(screen.getByTestId("profile-name").textContent).toBe("not reported");
    expect(screen.getByTestId("profile-role").textContent).toBe("user");
  });
});
