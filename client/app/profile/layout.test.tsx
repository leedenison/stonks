import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { renderWithAuth } from "@/lib/test-utils";
import ProfileLayout from "./layout";

const router = { replace: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

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

function transportAnswering(answer: () => Promise<typeof live> | typeof live) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: answer });
  });
}

describe("ProfileLayout", () => {
  beforeEach(() => router.replace.mockReset());

  it("shows a skeleton while restoring, then the page", async () => {
    const transport = transportAnswering(() => live);
    renderWithAuth(
      <ProfileLayout>
        <p>child</p>
      </ProfileLayout>,
      transport,
    );
    expect(screen.getByTestId("skeleton")).toBeTruthy();
    await waitFor(() => expect(screen.getByText("child")).toBeTruthy());
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("redirects to the landing page without a session", async () => {
    const transport = transportAnswering(() =>
      create(GetSessionResponseSchema, {}),
    );
    renderWithAuth(
      <ProfileLayout>
        <p>child</p>
      </ProfileLayout>,
      transport,
    );
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/"));
    expect(screen.queryByText("child")).toBeNull();
  });
});
