"use client";

import type { ReactNode } from "react";
import { SessionGuard } from "@/app/components/session-guard";
import { Sidebar } from "@/app/components/sidebar";
import { UploadDialog } from "@/app/components/upload-dialog";
import { UploadProvider, useUpload } from "@/contexts/upload-context";

// The segment every page needing a session lives under. The group adds no
// URL segment, so the pages keep their paths. The upload dialog is mounted
// once here, so any page can open it.
export default function AppLayout({ children }: { children: ReactNode }) {
  return (
    <SessionGuard>
      <UploadProvider>
        <Shell>{children}</Shell>
      </UploadProvider>
    </SessionGuard>
  );
}

function Shell({ children }: { children: ReactNode }) {
  const upload = useUpload();
  return (
    <div className="flex min-h-[calc(100dvh-var(--top-bar-height))]">
      <Sidebar />
      <main className="min-w-0 flex-1 animate-fade-in">{children}</main>
      <UploadDialog
        key={upload.state.key}
        open={upload.state.open}
        initial={upload.state.file}
        onClose={upload.close}
      />
    </div>
  );
}
