"use client";

import type { ReactNode } from "react";
import { SessionGuard } from "@/app/components/session-guard";
import { Sidebar } from "@/app/components/sidebar";

// The segment every page needing a session lives under. The group adds no
// URL segment, so the pages keep their paths.
export default function AppLayout({ children }: { children: ReactNode }) {
  return (
    <SessionGuard>
      <div className="flex min-h-[calc(100dvh-var(--top-bar-height))]">
        <Sidebar />
        <main className="min-w-0 flex-1 animate-fade-in px-6 py-6">
          {children}
        </main>
      </div>
    </SessionGuard>
  );
}
