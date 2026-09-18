"use client";

import { EmptyState } from "@/app/components/empty-state";
import { Page } from "@/app/components/page-frame";

export default function TransactionsPage() {
  return (
    <Page title="Transactions" width="wide" testId="transactions-page">
      <EmptyState message="No transactions yet. Upload a statement to fill this table." />
    </Page>
  );
}
