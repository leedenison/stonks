"use client";

import { Fragment, useState } from "react";
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
import { type HoldingRow, hasDetail, holdingRows } from "@/lib/holdings";
import { toFixed } from "@/lib/marshal/decimal";
import { HoldingDetail } from "./holding-detail";

// The user's holdings, cash first, each with its raw quantity shown to two
// places. A click on a row that has detail opens it beneath the row, closing
// any other.
export default function HoldingsPage() {
  const upload = useUpload();
  const { data, isPending, isError, refetch } = useHoldings();
  const holdings = holdingRows(data);
  const [openId, setOpenId] = useState<string | null>(null);

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
              <Th>Currency</Th>
              <Th numeric>Quantity</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={4} />
          ) : (
            <tbody>
              {holdings.map((h) => {
                const opens = hasDetail(h);
                const open = opens && openId === h.id;
                return (
                  <Fragment key={h.id}>
                    <Tr
                      data-testid={`holding-row-${h.id}`}
                      data-kind={h.kind}
                      aria-expanded={opens ? open : undefined}
                      onClick={
                        opens ? () => setOpenId(open ? null : h.id) : undefined
                      }
                      className={opens ? "cursor-pointer" : ""}
                    >
                      <Td>
                        <Name row={h} />
                      </Td>
                      <Td>
                        {h.classes.map((c) => (
                          <Chip key={c}>{assetClassLabel(c)}</Chip>
                        ))}
                      </Td>
                      <Td
                        className="font-mono tabular-nums"
                        data-testid={`holding-currency-${h.id}`}
                      >
                        {h.currencies.join(" ")}
                      </Td>
                      <Td numeric data-testid={`holding-qty-${h.id}`}>
                        {toFixed(h.quantity, 2)}
                      </Td>
                    </Tr>
                    {open && <HoldingDetail row={h} columns={4} />}
                  </Fragment>
                );
              })}
            </tbody>
          )}
        </TableCard>
      )}
    </Page>
  );
}

// Name shows a holding's label with its venue, and marks a group
// unidentified.
function Name({ row }: { row: HoldingRow }) {
  return (
    <span className="flex items-center gap-2">
      <span>{row.label}</span>
      {row.venue && (
        <span
          className="text-text-muted"
          data-testid={`holding-venue-${row.id}`}
        >
          {row.venue}
        </span>
      )}
      {row.kind === "group" && (
        <Chip data-testid="holding-basis" data-state="unidentified">
          Unidentified
        </Chip>
      )}
    </span>
  );
}
