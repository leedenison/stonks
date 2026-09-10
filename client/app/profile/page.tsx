"use client";

import { useAuth } from "@/contexts/auth-context";
import { formatInstant } from "@/lib/format";
import { roleLabel } from "@/lib/role";

// The signed-in user's account, read-only. The layout guarantees a session.
export default function ProfilePage() {
  const { state } = useAuth();
  if (state.status !== "authenticated") {
    return null;
  }
  const { user, session } = state;
  const expires = session.expiresAt ? formatInstant(session.expiresAt) : "";

  return (
    <section data-testid="profile-page" className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold tracking-tight">Profile</h1>
      <dl className="grid max-w-md grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm">
        <dt className="text-text-muted">Email</dt>
        <dd data-testid="profile-email">{user.email}</dd>
        <dt className="text-text-muted">Name</dt>
        <dd data-testid="profile-name">
          {user.name || <span className="text-text-muted">not reported</span>}
        </dd>
        <dt className="text-text-muted">Role</dt>
        <dd data-testid="profile-role">{roleLabel(user.role)}</dd>
        <dt className="text-text-muted">Session expires</dt>
        <dd data-testid="profile-expires" className="font-mono tabular-nums">
          {expires}
        </dd>
      </dl>
    </section>
  );
}
