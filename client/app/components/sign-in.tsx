"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { GoogleLogin } from "@react-oauth/google";
import { useState } from "react";
import { useAuth } from "@/contexts/auth-context";

// SignIn renders the Google button and hands the ID token it yields to the
// service. Google fills the button in itself, so the test id sits on the
// wrapper.
export function SignIn() {
  const { signIn } = useAuth();
  const [noCredential, setNoCredential] = useState(false);
  const error = noCredential
    ? "Google did not return a credential. Try again."
    : signInMessage(signIn.error);

  return (
    <div data-testid="sign-in" className="flex flex-col gap-2">
      <GoogleLogin
        useOneTap={false}
        onSuccess={({ credential }) => {
          if (!credential) {
            setNoCredential(true);
            return;
          }
          setNoCredential(false);
          signIn.mutate(credential);
        }}
        onError={() => setNoCredential(true)}
      />
      {error && (
        <p
          data-testid="sign-in-error"
          role="alert"
          className="text-sm text-negative"
        >
          {error}
        </p>
      )}
    </div>
  );
}

function signInMessage(err: Error | null): string | null {
  if (!err) {
    return null;
  }
  if (err instanceof ConnectError && err.code === Code.PermissionDenied) {
    return "This Google account is not permitted to sign in.";
  }
  return "Sign-in failed. Try again.";
}
