"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { useCoverageDomains, useCoverageGaps } from "@/hooks/use-coverage";
import { type CoverageParams, coverageQuery, readCoverage } from "@/lib/admin";
import { TickerPane } from "./ticker-pane";

// CoveragePage lists the gaps of one operating MIC's tickers, by what they
// affect, with the selected ticker in a pane beside the list.
export default function CoveragePage() {
  return (
    <Suspense>
      <Coverage />
    </Suspense>
  );
}

function Coverage() {
  const p = readCoverage(useSearchParams());
  const router = useRouter();
  const domains = useCoverageDomains();
  const domain = p.domain || domains.data?.domains[0]?.mic || "";
  const gaps = useCoverageGaps(domain);
  const go = (next: Partial<CoverageParams>) =>
    router.replace(coverageQuery({ ...p, domain, ...next }));
  const listed = gaps.data?.gaps ?? [];
  const failed = domains.isError || gaps.isError;
  const loading = domains.isPending || (domain !== "" && gaps.isPending);

  return (
    <Page title="Coverage" width="wide" testId="admin-coverage-page">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <span className="text-text-muted">Domain</span>
          <Select
            data-testid="coverage-filter-domain"
            value={domain}
            onChange={(e) => go({ domain: e.target.value, value: "" })}
          >
            {domains.data?.domains.map((d) => (
              <option key={d.mic} value={d.mic}>
                {d.mic} ({d.tickers})
              </option>
            ))}
          </Select>
        </label>
      </div>
      {failed && (
        <Notice
          tone="error"
          onRetry={() => {
            if (domains.isError) domains.refetch();
            if (gaps.isError) gaps.refetch();
          }}
        >
          The coverage could not be loaded.
        </Notice>
      )}
      {!failed && domains.data && domain === "" && (
        <EmptyState message="No key states a ticker." />
      )}
      {!failed && domain !== "" && (
        <div className="flex items-start gap-4">
          <div className="flex w-[30rem] shrink-0 flex-col gap-4">
            {!loading && listed.length === 0 ? (
              <EmptyState message={`No gaps on ${domain}.`} />
            ) : (
              <TableCard testId="coverage-gaps-table">
                <Thead>
                  <tr>
                    <Th>Ticker</Th>
                    <Th>From</Th>
                    <Th>Before</Th>
                    <Th numeric>Keys</Th>
                    <Th numeric>Transactions</Th>
                  </tr>
                </Thead>
                {loading ? (
                  <SkeletonRows columns={5} />
                ) : (
                  <tbody>
                    {listed.map((g) => {
                      const value = g.ticker?.value ?? "";
                      const href = coverageQuery({ domain, value });
                      const open = value === p.value;
                      return (
                        <Tr
                          key={`${value}-${g.unconfirmedFrom}`}
                          data-testid={`gap-row-${value}-${g.unconfirmedFrom}`}
                          href={href}
                          aria-selected={open}
                          className={open ? "bg-primary-light/15" : ""}
                        >
                          <Td>
                            <Link href={href}>
                              <Chip tone="primary" className="font-mono">
                                {value}
                              </Chip>
                            </Link>
                          </Td>
                          <Td className="font-mono tabular-nums">
                            {g.unconfirmedFrom}
                          </Td>
                          <Td className="font-mono tabular-nums">
                            {g.unconfirmedBefore}
                          </Td>
                          <Td numeric>{g.keys}</Td>
                          <Td numeric>{g.transactions}</Td>
                        </Tr>
                      );
                    })}
                  </tbody>
                )}
              </TableCard>
            )}
          </div>
          {p.value ? (
            <TickerPane domain={domain} value={p.value} />
          ) : (
            <p className="flex-1 text-text-muted">Select a gap.</p>
          )}
        </div>
      )}
    </Page>
  );
}
