"use client";

import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { IdentifierChips } from "@/app/components/identifier-chip";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { UploadAction } from "@/app/components/upload-action";
import { useUpload } from "@/contexts/upload-context";
import { useHoldings } from "@/hooks/use-holdings";
import { assetClassLabel } from "@/lib/asset-class";
import { brokerLabel } from "@/lib/broker";
import { type HoldingRow, holdingRows } from "@/lib/holdings";
import { toFixed } from "@/lib/marshal/decimal";

// The user's holdings, cash first, each with its raw quantity shown to two
// places. A holding of an instrument is named by the instrument; a holding
// of unresolved keys is named by what they state and marked as resting on
// the user's statements alone.
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
          message="No holdings yet."
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
                <Tr
                  key={h.id}
                  data-testid={`holding-row-${h.id}`}
                  data-kind={h.kind}
                >
                  <Td>
                    <Name row={h} />
                  </Td>
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

// Name shows a holding's label with its identifiers, and for a group, its
// descriptions and the statements-only mark.
function Name({ row }: { row: HoldingRow }) {
  const described = row.kind === "group" ? row.descriptions : [];
  return (
    <span className="flex flex-col gap-1">
      <span className="flex items-center gap-2">
        <span>{row.label}</span>
        {row.kind === "group" && (
          <Chip data-testid="holding-basis" data-state="statements">
            Statements only
          </Chip>
        )}
      </span>
      {(row.identifiers.length > 0 || described.length > 0) && (
        <span className="flex flex-wrap gap-1">
          <IdentifierChips ids={row.identifiers} />
          {described.map((d) => (
            <Chip key={`${d.broker}-${d.text}`}>
              <span className="font-semibold">{brokerLabel(d.broker)}</span>
              <span>{d.text}</span>
            </Chip>
          ))}
        </span>
      )}
    </span>
  );
}
