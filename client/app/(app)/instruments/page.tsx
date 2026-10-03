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
import { useInstruments } from "@/hooks/use-instruments";
import { assetClassLabel } from "@/lib/asset-class";
import { instrumentRows } from "@/lib/instruments";

// The instruments the user's keys resolved to, each with its listings.
export default function InstrumentsPage() {
  const upload = useUpload();
  const { data, isPending, isError, refetch } = useInstruments();
  const instruments = instrumentRows(data);

  return (
    <Page
      title="Instruments"
      width="wide"
      testId="instruments-page"
      actions={<UploadAction />}
    >
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The instruments could not be loaded.
        </Notice>
      )}
      {!isError && data && instruments.length === 0 && (
        <EmptyState
          message="No instruments yet."
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
      {!isError && (isPending || instruments.length > 0) && (
        <TableCard testId="instruments-table">
          <Thead>
            <tr>
              <Th>Instrument</Th>
              <Th>Class</Th>
              <Th>Listings</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={3} />
          ) : (
            <tbody>
              {instruments.map((i) => (
                <Tr key={i.id} data-testid={`instrument-row-${i.id}`}>
                  <Td>
                    <span className="flex flex-col gap-1">
                      <span>{i.label}</span>
                      {i.identifiers.length > 0 && (
                        <IdentifierChips ids={i.identifiers} />
                      )}
                    </span>
                  </Td>
                  <Td>
                    <Chip>{assetClassLabel(i.assetClass)}</Chip>
                  </Td>
                  <Td>
                    <span className="flex flex-col gap-1">
                      {i.listings.map((l) => (
                        <span
                          key={l.id}
                          data-testid={`listing-${l.id}`}
                          className="flex flex-wrap items-center gap-1"
                        >
                          <span className="font-mono tabular-nums">
                            {l.currency}
                          </span>
                          <IdentifierChips ids={l.identifiers} />
                        </span>
                      ))}
                    </span>
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
