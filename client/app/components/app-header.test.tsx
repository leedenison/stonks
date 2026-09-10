import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  SignOutResponseSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { renderWithAuth } from "@/lib/test-utils";
import { AppHeader } from "./app-header";

const live = create(GetSessionResponseSchema, {
  user: create(UserSchema, {
    id: "u1",
    email: "a@example.com",
    role: Role.USER,
  }),
  session: create(SessionSchema, {
    expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
  }),
});

describe("AppHeader", () => {
  it("names the product and shows a visitor as not signed in", async () => {
    const transport = createRouterTransport(({ service }) => {
      service(AuthService, {
        getSession: () => create(GetSessionResponseSchema, {}),
      });
    });
    renderWithAuth(<AppHeader />, transport);
    expect(
      screen.getByRole("link", { name: "Stonks" }).getAttribute("href"),
    ).toBe("/");
    await waitFor(() =>
      expect(screen.getByTestId("user-area").textContent).toBe("Not signed in"),
    );
  });

  it("shows the signed-in user and signs out", async () => {
    const signOut = vi.fn(() => create(SignOutResponseSchema, {}));
    const transport = createRouterTransport(({ service }) => {
      service(AuthService, { getSession: () => live, signOut });
    });
    renderWithAuth(<AppHeader />, transport);
    await waitFor(() =>
      expect(screen.getByTestId("user-email").textContent).toBe(
        "a@example.com",
      ),
    );
    expect(screen.getByTestId("user-email").getAttribute("href")).toBe(
      "/profile",
    );
    fireEvent.click(screen.getByTestId("sign-out"));
    await waitFor(() => expect(signOut).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByTestId("user-area").textContent).toBe("Not signed in"),
    );
  });
});
