"use client";

import { EmptyState } from "@/app/components/empty-state";
import { Page } from "@/app/components/page-frame";

export default function StatementsPage() {
  return (
    <Page title="Statements" width="wide" testId="statements-page">
      <EmptyState message="No statements yet. Each upload is listed here with its outcome." />
    </Page>
  );
}
