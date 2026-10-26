"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { Pager } from "@/app/components/admin-list";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Input, Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { useAdminInstruments } from "@/hooks/use-admin-instruments";
import { useDebounced } from "@/hooks/use-debounced";
import {
  type InstrumentParams,
  instrumentQuery,
  readInstrumentList,
} from "@/lib/admin";
import { assetClassLabel } from "@/lib/asset-class";
import { enumParam, enumValues } from "@/lib/enum";
import { InstrumentPane } from "./instrument-pane";

// The search box waits this long after the last keystroke before it
// updates the address and the list.
const searchDelay = 300;

// The instruments by name, a page at a time, with the selected one in a
// pane beside the table. The address carries the search, the class and the
// page, the selected row of that page, and the pane's tab and ticker.
export default function InstrumentsPage() {
  return (
    <Suspense>
      <Instruments />
    </Suspense>
  );
}

function Instruments() {
  const p = readInstrumentList(useSearchParams());
  const router = useRouter();
  const { data, isPending, isError, refetch } = useAdminInstruments(p);
  const instruments = data?.instruments ?? [];
  const selected = instruments.find((i) => i.id === p.id);
  const go = (next: Partial<InstrumentParams>) =>
    router.replace(
      instrumentQuery({ ...p, after: "", id: "", ticker: "", ...next }),
    );
  const [text, setText] = useState(p.q);
  const settled = useDebounced(text, searchDelay);
  const searched = instrumentQuery({
    ...p,
    after: "",
    id: "",
    ticker: "",
    q: settled,
  });
  useEffect(() => {
    if (settled !== p.q) {
      router.replace(searched);
    }
  }, [settled, p.q, searched, router]);

  return (
    <Page title="Instruments" width="wide" testId="admin-instruments-page">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <Input
          type="search"
          data-testid="instruments-search"
          placeholder="Search identifiers"
          aria-label="Search identifiers"
          value={text}
          onChange={(e) => setText(e.target.value)}
          className="w-64"
        />
        <label className="flex items-center gap-2">
          <span className="text-text-muted">Class</span>
          <Select
            data-testid="instruments-filter-class"
            value={p.class}
            onChange={(e) => go({ class: e.target.value })}
          >
            <option value="">any</option>
            {enumValues(AssetClass).map((v) => (
              <option key={v} value={enumParam(AssetClass, v)}>
                {assetClassLabel(v)}
              </option>
            ))}
          </Select>
        </label>
      </div>
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
                    const href = instrumentQuery({
                      ...p,
                      id: i.id,
                      ticker: "",
                    });
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
                            {i.name?.value || i.id}
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
                p.after
                  ? instrumentQuery({ ...p, after: "", id: "", ticker: "" })
                  : undefined
              }
              next={
                data?.nextPageToken
                  ? instrumentQuery({
                      ...p,
                      after: data.nextPageToken,
                      id: "",
                      ticker: "",
                    })
                  : undefined
              }
              labels={{ first: "First", next: "Next" }}
            />
          </div>
          {selected ? (
            <InstrumentPane instrument={selected} params={p} />
          ) : (
            <p className="flex-1 text-text-muted">Select an instrument.</p>
          )}
        </div>
      )}
    </Page>
  );
}
