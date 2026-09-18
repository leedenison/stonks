"use client";

import type { ReactNode } from "react";
import { AccessDenied } from "@/app/components/access-denied";
import { AdminNav } from "@/app/components/admin-nav";
import { SessionGuard } from "@/app/components/session-guard";
import { useAuth } from "@/contexts/auth-context";
import { Role } from "@/gen/auth/v1/auth_pb";

// The admin area shares the top bar and has its own navigation. A user
// without the admin role sees access denied under the top bar.
export default function AdminLayout({ children }: { children: ReactNode }) {
  return (
    <SessionGuard>
      <Gate>{children}</Gate>
    </SessionGuard>
  );
}

function Gate({ children }: { children: ReactNode }) {
  const { state } = useAuth();
  if (state.status !== "authenticated" || state.user.role !== Role.ADMIN) {
    return (
      <main className="mx-auto max-w-6xl px-4 py-6">
        <AccessDenied />
      </main>
    );
  }
  return (
    <div className="flex min-h-[calc(100dvh-var(--top-bar-height))]">
      <AdminNav />
      <main className="min-w-0 flex-1 animate-fade-in">{children}</main>
    </div>
  );
}
