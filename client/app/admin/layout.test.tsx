import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { renderWithAuth } from "@/lib/test-utils";
import AdminLayout from "./layout";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
  usePathname: () => "/admin",
}));

function transportFor(role: Role) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getSession: () =>
        create(GetSessionResponseSchema, {
          user: create(UserSchema, { id: "u1", email: "a@example.com", role }),
          session: create(SessionSchema, {
            expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
          }),
        }),
    });
  });
}

describe("AdminLayout", () => {
  it("denies a user", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportFor(Role.USER),
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
      transportFor(Role.ADMIN),
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
