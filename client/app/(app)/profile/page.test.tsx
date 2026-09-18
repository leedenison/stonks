import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { renderWithAuth } from "@/lib/test-utils";
import ProfilePage from "./page";

function transportFor(name: string, role: Role) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getSession: () =>
        create(GetSessionResponseSchema, {
          user: create(UserSchema, {
            id: "u1",
            email: "a@example.com",
            name,
            role,
          }),
          session: create(SessionSchema, {
            expiresAt: timestampFromDate(new Date("2026-09-16T08:30:00Z")),
          }),
        }),
    });
  });
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
