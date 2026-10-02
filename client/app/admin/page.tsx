"use client";

import { Page } from "@/app/components/page-frame";

export default function AdminPage() {
  return (
    <Page title="Admin" testId="admin-page">
      <p className="text-text-muted">
        Runs, datasources and blocks are listed in the navigation. The findings
        of a run are on its page.
      </p>
    </Page>
  );
}
