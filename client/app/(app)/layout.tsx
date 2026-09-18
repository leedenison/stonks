"use client";

import type { ReactNode } from "react";
import { SessionGuard } from "@/app/components/session-guard";

// The segment every page needing a session lives under. The group adds no
// URL segment, so the pages keep their paths.
export default function AppLayout({ children }: { children: ReactNode }) {
  return (
    <SessionGuard>
      <main className="mx-auto max-w-6xl px-4 py-6">{children}</main>
    </SessionGuard>
  );
}
