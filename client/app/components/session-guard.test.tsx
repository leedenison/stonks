import { create } from "@bufbuild/protobuf";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GetSessionResponseSchema } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import { SessionGuard } from "./session-guard";

const router = { replace: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const live = liveSession();

describe("SessionGuard", () => {
  beforeEach(() => router.replace.mockReset());

  it("shows a skeleton while restoring, then the children", async () => {
    renderWithAuth(
      <SessionGuard>
        <p>child</p>
      </SessionGuard>,
      transportWith(live),
    );
    expect(screen.getByTestId("skeleton")).toBeTruthy();
    await waitFor(() => expect(screen.getByText("child")).toBeTruthy());
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("redirects to the landing page without a session", async () => {
    renderWithAuth(
      <SessionGuard>
        <p>child</p>
      </SessionGuard>,
      transportWith(create(GetSessionResponseSchema, {})),
    );
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/"));
    expect(screen.queryByText("child")).toBeNull();
  });
});
