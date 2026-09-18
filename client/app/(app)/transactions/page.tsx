"use client";

import { useState } from "react";
import { Button } from "@/app/components/button";
import { EmptyState } from "@/app/components/empty-state";
import { Page } from "@/app/components/page-frame";
import { UploadAction } from "@/app/components/upload-action";
import { useUpload } from "@/contexts/upload-context";

// The transactions page is a drop target: an export dropped anywhere on it
// opens the upload dialog with that file.
export default function TransactionsPage() {
  const upload = useUpload();
  const [over, setOver] = useState(false);
  return (
    <div
      data-testid="transactions-drop"
      data-over={over || undefined}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        const file = e.dataTransfer.files[0];
        if (file) {
          upload.open(file);
        }
      }}
      className="min-h-full transition-colors data-over:bg-primary-light/15"
    >
      <Page
        title="Transactions"
        width="wide"
        testId="transactions-page"
        actions={<UploadAction />}
      >
        <EmptyState
          message="No transactions yet. Upload a statement to fill this table."
          action={
            <Button
              data-testid="upload-statement-empty"
              onClick={() => upload.open()}
            >
              Upload statement
            </Button>
          }
        />
      </Page>
    </div>
  );
}
