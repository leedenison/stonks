"use client";

import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { UploadAction } from "@/app/components/upload-action";
import { useUpload } from "@/contexts/upload-context";
import { useHoldings } from "@/hooks/use-holdings";
import { assetClassLabel } from "@/lib/asset-class";
import { holdingRows } from "@/lib/holdings";
import { toFixed } from "@/lib/marshal/decimal";

// The user's holdings, cash first, each with its raw quantity shown to two
// places.
export default function HoldingsPage() {
  const upload = useUpload();
  const { data, isPending, isError, refetch } = useHoldings();
  const holdings = holdingRows(data);

  return (
    <Page
      title="Holdings"
      width="wide"
      testId="holdings-page"
      actions={<UploadAction />}
    >
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The holdings could not be loaded.
        </Notice>
      )}
      {!isError && data && holdings.length === 0 && (
        <EmptyState
          message="No holdings yet. Holdings are derived from the transactions a statement supplies."
          action={
            <Button
              data-testid="upload-statement-empty"
              onClick={() => upload.open()}
            >
              Upload statement
            </Button>
          }
        />
      )}
      {!isError && (isPending || holdings.length > 0) && (
        <TableCard testId="holdings-table">
          <Thead>
            <tr>
              <Th>Instrument</Th>
              <Th>Class</Th>
              <Th numeric>Quantity</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={3} />
          ) : (
            <tbody>
              {holdings.map((h) => (
                <Tr key={h.id} data-testid={`holding-row-${h.id}`}>
                  <Td>{h.label}</Td>
                  <Td>
                    {h.classes.map((c) => (
                      <Chip key={c}>{assetClassLabel(c)}</Chip>
                    ))}
                  </Td>
                  <Td numeric data-testid={`holding-qty-${h.id}`}>
                    {toFixed(h.quantity, 2)}
                  </Td>
                </Tr>
              ))}
            </tbody>
          )}
        </TableCard>
      )}
    </Page>
  );
}
