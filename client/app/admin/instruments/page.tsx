"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { Pager } from "@/app/components/admin-list";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { useAdminInstruments } from "@/hooks/use-admin-instruments";
import { instrumentQuery, readInstrumentList } from "@/lib/admin";
import { assetClassLabel } from "@/lib/asset-class";
import { InstrumentPane } from "./instrument-pane";

// Every instrument by name, a page at a time, with the selected one in a
// pane beside the table. The address carries the page and the selection.
export default function InstrumentsPage() {
  return (
    <Suspense>
      <Instruments />
    </Suspense>
  );
}

function Instruments() {
  const p = readInstrumentList(useSearchParams());
  const { data, isPending, isError, refetch } = useAdminInstruments(p.after);
  const instruments = data?.instruments ?? [];
  const selected = instruments.find((i) => i.id === p.id);

  return (
    <Page title="Instruments" width="wide" testId="admin-instruments-page">
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The instruments could not be loaded.
        </Notice>
      )}
      {!isError && data && instruments.length === 0 && (
        <EmptyState message="No instruments." />
      )}
      {!isError && (isPending || instruments.length > 0) && (
        <div className="flex items-start gap-4">
          <div className="flex w-[24rem] shrink-0 flex-col gap-4">
            <TableCard testId="admin-instruments-table">
              <Thead>
                <tr>
                  <Th>Instrument</Th>
                  <Th>Class</Th>
                </tr>
              </Thead>
              {isPending ? (
                <SkeletonRows columns={2} />
              ) : (
                <tbody>
                  {instruments.map((i) => {
                    const href = instrumentQuery({ after: p.after, id: i.id });
                    const open = i.id === p.id;
                    return (
                      <Tr
                        key={i.id}
                        data-testid={`instrument-row-${i.id}`}
                        href={href}
                        aria-selected={open}
                        className={open ? "bg-primary-light/15" : ""}
                      >
                        <Td>
                          <Link
                            href={href}
                            className={
                              i.name ? "" : "font-mono text-text-muted"
                            }
                          >
                            {i.name || i.id}
                          </Link>
                        </Td>
                        <Td>
                          <Chip>{assetClassLabel(i.assetClass)}</Chip>
                        </Td>
                      </Tr>
                    );
                  })}
                </tbody>
              )}
            </TableCard>
            <Pager
              first={
                p.after ? instrumentQuery({ after: "", id: "" }) : undefined
              }
              next={
                data?.nextPageToken
                  ? instrumentQuery({ after: data.nextPageToken, id: "" })
                  : undefined
              }
              labels={{ first: "First", next: "Next" }}
            />
          </div>
          {selected ? (
            <InstrumentPane instrument={selected} />
          ) : (
            <p className="flex-1 text-text-muted">Select an instrument.</p>
          )}
        </div>
      )}
    </Page>
  );
}
