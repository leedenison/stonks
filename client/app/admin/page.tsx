"use client";

import { Page } from "@/app/components/page-frame";

export default function AdminPage() {
  return (
    <Page title="Admin" testId="admin-page">
      <p className="text-text-muted">
        Runs, findings and datasources appear here as they are built.
      </p>
    </Page>
  );
}
