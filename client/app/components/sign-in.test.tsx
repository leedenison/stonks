import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createRouterTransport,
  type ServiceImpl,
} from "@connectrpc/connect";
import type { CredentialResponse } from "@react-oauth/google";
import { act, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  SignInResponseSchema,
} from "@/gen/auth/v1/auth_pb";
import { renderWithAuth } from "@/lib/test-utils";
import { SignIn } from "./sign-in";

// Google's button is an iframe it fills in itself; the mock exposes the
// callback the real one would invoke.
const google = vi.hoisted(() => ({
  onSuccess: null as ((r: CredentialResponse) => void) | null,
}));
vi.mock("@react-oauth/google", () => ({
  GoogleLogin: ({
    onSuccess,
  }: {
    onSuccess: (r: CredentialResponse) => void;
  }) => {
    google.onSuccess = onSuccess;
    return <div data-testid="google-button" />;
  },
}));

function transportWith(signIn: ServiceImpl<typeof AuthService>["signIn"]) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getSession: () => create(GetSessionResponseSchema, {}),
      signIn,
    });
  });
}

describe("SignIn", () => {
  it("sends the credential to the service", async () => {
    const signIn = vi.fn(() => create(SignInResponseSchema, {}));
    renderWithAuth(<SignIn />, transportWith(signIn));
    await waitFor(() => expect(google.onSuccess).not.toBeNull());
    act(() => google.onSuccess?.({ credential: "id-token" }));
    await waitFor(() => expect(signIn).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("sign-in-error")).toBeNull();
  });

  it("explains a refused account", async () => {
    renderWithAuth(
      <SignIn />,
      transportWith(() => {
        throw new ConnectError("email not permitted", Code.PermissionDenied);
      }),
    );
    await waitFor(() => expect(google.onSuccess).not.toBeNull());
    act(() => google.onSuccess?.({ credential: "id-token" }));
    await waitFor(() =>
      expect(screen.getByTestId("sign-in-error").textContent).toMatch(
        /not permitted/,
      ),
    );
  });

  it("reports a missing credential without calling the service", async () => {
    const signIn = vi.fn(() => create(SignInResponseSchema, {}));
    renderWithAuth(<SignIn />, transportWith(signIn));
    await waitFor(() => expect(google.onSuccess).not.toBeNull());
    act(() => google.onSuccess?.({}));
    await waitFor(() =>
      expect(screen.getByTestId("sign-in-error").textContent).toMatch(
        /credential/,
      ),
    );
    expect(signIn).not.toHaveBeenCalled();
  });
});
