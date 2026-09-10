"use client";

import Link from "next/link";
import { useAuth } from "@/contexts/auth-context";

export function AppHeader() {
  const { state, signOut } = useAuth();

  return (
    <header
      data-testid="app-header"
      className="border-b border-border bg-surface"
    >
      <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
        <Link
          href="/"
          className="font-display text-lg font-semibold tracking-tight"
        >
          Stonks
        </Link>
        <div
          data-testid="user-area"
          className="flex items-center gap-4 text-sm text-text-muted"
        >
          {state.status === "unauthenticated" && <span>Not signed in</span>}
          {state.status === "authenticated" && (
            <>
              <Link
                href="/profile"
                data-testid="user-email"
                className="text-text-primary hover:underline"
              >
                {state.user.email}
              </Link>
              {/* The profile guard redirects once the session is gone. */}
              <button
                type="button"
                data-testid="sign-out"
                disabled={signOut.isPending}
                onClick={() => signOut.mutate()}
                className="rounded border border-border px-2 py-1 text-xs font-medium text-text-primary hover:bg-background disabled:opacity-50"
              >
                Sign out
              </button>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
