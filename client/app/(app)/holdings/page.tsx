"use client";

import { EmptyState } from "@/app/components/empty-state";
import { Page } from "@/app/components/page-frame";

export default function HoldingsPage() {
  return (
    <Page title="Holdings" width="wide" testId="holdings-page">
      <EmptyState message="No holdings yet. Holdings are derived from the transactions a statement supplies." />
    </Page>
  );
}
